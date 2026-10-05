// One-shot, private-staging WebView2 renderer. No workspace bridge or network content.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <aclapi.h>
#include <appmodel.h>
#include <shobjidl.h>
#include <bcrypt.h>
#include <shlwapi.h>
#include <wrl.h>
#include <wrl/event.h>
#include <fcntl.h>
#include <io.h>
#include <chrono>
#include <climits>
#include <cstdio>
#include <memory>
#include <thread>
#include "WebView2.h"
#include "WebView2EnvironmentOptions.h"
#include "document-policy.hpp"

using Microsoft::WRL::ComPtr;
using Microsoft::WRL::Callback;
using namespace trove_document;
namespace {
constexpr wchar_t OriginW[] = L"https://trove-document.invalid/";
constexpr UINT Tick = 1;
struct Handle {
    HANDLE value=INVALID_HANDLE_VALUE;
    explicit Handle(HANDLE h=INVALID_HANDLE_VALUE):value(h){}
    ~Handle(){if(value!=INVALID_HANDLE_VALUE&&value!=nullptr)CloseHandle(value);}
    Handle(const Handle&)=delete;Handle& operator=(const Handle&)=delete;
    Handle(Handle&& h)noexcept:value(h.value){h.value=INVALID_HANDLE_VALUE;}
    Handle& operator=(Handle&& h)noexcept{if(value!=INVALID_HANDLE_VALUE)CloseHandle(value);value=h.value;h.value=INVALID_HANDLE_VALUE;return *this;}
};
void checked(HRESULT hr){require(SUCCEEDED(hr));}
std::string utf8(const wchar_t *value,size_t maximum=MaxStatus){
    require(value!=nullptr);size_t count=0;while(value[count]&&count<=maximum)++count;require(count<=maximum);
    if(count==0)return {};
    int size=WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,value,int(count),nullptr,0,nullptr,nullptr);
    require(size>0&&size_t(size)<=maximum);std::string result(size,'\0');
    require(WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,value,int(count),result.data(),size,nullptr,nullptr)==size);return result;
}
std::wstring wide(const std::string &value){
    if(value.empty())return {};
    int size=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),nullptr,0);require(size>0);
    std::wstring result(size,L'\0');require(MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),result.data(),size)==size);return result;
}
struct COMString { LPWSTR value=nullptr;~COMString(){CoTaskMemFree(value);} };
std::string sha256(const std::string &bytes){
    BCRYPT_ALG_HANDLE algorithm=nullptr;BCRYPT_HASH_HANDLE hash=nullptr;
    require(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,nullptr,0)==0);
    struct CloseAlgorithm {BCRYPT_ALG_HANDLE a;~CloseAlgorithm(){BCryptCloseAlgorithmProvider(a,0);}} closeAlgorithm{algorithm};
    require(BCryptCreateHash(algorithm,&hash,nullptr,0,nullptr,0,0)==0);
    struct CloseHash {BCRYPT_HASH_HANDLE h;~CloseHash(){BCryptDestroyHash(h);}} closeHash{hash};
    require(bytes.size()<=ULONG_MAX&&BCryptHashData(hash,reinterpret_cast<PUCHAR>(const_cast<char*>(bytes.data())),ULONG(bytes.size()),0)==0);
    unsigned char result[32];require(BCryptFinishHash(hash,result,sizeof result,0)==0);std::string hex;hex.reserve(64);
    for(unsigned char c:result){hex.push_back("0123456789abcdef"[c>>4]);hex.push_back("0123456789abcdef"[c&15]);}return hex;
}
uint64_t file_size(HANDLE h){LARGE_INTEGER size{};require(GetFileSizeEx(h,&size)&&size.QuadPart>=0);return uint64_t(size.QuadPart);}
void regular(HANDLE h){
    BY_HANDLE_FILE_INFORMATION info{};require(GetFileInformationByHandle(h,&info));
    require(!(info.dwFileAttributes&(FILE_ATTRIBUTE_DIRECTORY|FILE_ATTRIBUTE_REPARSE_POINT))&&info.nNumberOfLinks==1&&GetFileType(h)==FILE_TYPE_DISK);
}
std::string read_file(HANDLE h,size_t maximum){
    regular(h);auto size=file_size(h);require(size<=maximum);std::string bytes(size,'\0');LARGE_INTEGER zero{};
    require(SetFilePointerEx(h,zero,nullptr,FILE_BEGIN));
    for(size_t at=0;at<bytes.size();){DWORD got=0;auto chunk=DWORD(std::min<size_t>(65536,bytes.size()-at));require(ReadFile(h,bytes.data()+at,chunk,&got,nullptr)&&got>0);at+=got;}
    require(file_size(h)==size);return bytes;
}
void write_stdout(const std::string &bytes){
    HANDLE h=GetStdHandle(STD_OUTPUT_HANDLE);require(h!=INVALID_HANDLE_VALUE&&h!=nullptr);
    for(size_t at=0;at<bytes.size();){DWORD written=0;auto n=DWORD(std::min<size_t>(65536,bytes.size()-at));require(WriteFile(h,bytes.data()+at,n,&written,nullptr)&&written>0);at+=written;}
}
// Lock directory components without delete sharing, preventing a checked parent
// from being renamed/replaced while requests are served. Never follow reparses.
class Staging {
    std::vector<Handle> directories;
    std::map<std::string,Handle> files;
    std::wstring root;
public:
    Resources inventory;
    Handle result;
    std::string resultHash;
    std::wstring profile;
    explicit Staging(const std::wstring &path,const std::string &mode){
        require(path.size()>3&&path.size()<30000&&path[1]==L':'&&(path[2]==L'\\'||path[2]==L'/'));
        require(path.find(L':',2)==std::wstring::npos&&path.find(L'\0')==std::wstring::npos);
        std::vector<wchar_t> canonical(32768);DWORD length=GetFullPathNameW(path.c_str(),DWORD(canonical.size()),canonical.data(),nullptr);require(length>0&&length<canonical.size());
        root.assign(canonical.data(),length);while(root.size()>3&&root.back()==L'\\')root.pop_back();
        auto provided=path;std::replace(provided.begin(),provided.end(),L'/',L'\\');while(provided.size()>3&&provided.back()==L'\\')provided.pop_back();
        require(_wcsicmp(provided.c_str(),root.c_str())==0);
        lock_parents(root);
        private_directory(root);
        auto parent=root.substr(0,root.find_last_of(L'\\'));private_directory(parent);
        profile=parent+L"\\webview-profile";lock_parents(profile);private_directory(profile);
        // A fresh parent-owned profile cannot inherit prior cookies/extensions.
        WIN32_FIND_DATAW found{};HANDLE find=FindFirstFileW((profile+L"\\*").c_str(),&found);require(find!=INVALID_HANDLE_VALUE);
        bool empty=true;do{if(wcscmp(found.cFileName,L".")&&wcscmp(found.cFileName,L".."))empty=false;}while(FindNextFileW(find,&found));FindClose(find);require(empty);
        auto map=open("resource-map.json");inventory=resources(read_file(map.value,MaxMap),mode);
        for(const auto &entry:inventory){auto input=open(entry.first);auto bytes=read_file(input.value,entry.second.size);require(bytes.size()==entry.second.size&&sha256(bytes)==entry.second.sha256);files.emplace(entry.first,std::move(input));}
        require(GetFileAttributesW((root+L"\\result.pdf").c_str())==INVALID_FILE_ATTRIBUTES&&GetFileAttributesW((root+L"\\result.json").c_str())==INVALID_FILE_ATTRIBUTES);
    }
    std::wstring output()const{return root+L"\\result.pdf";}
    void lock_parents(const std::wstring &path){
        for(size_t at=3;;){auto end=path.find(L'\\',at);auto part=path.substr(0,end==std::wstring::npos?path.size():end);
            Handle handle(CreateFileW(part.c_str(),FILE_READ_ATTRIBUTES|READ_CONTROL,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,FILE_FLAG_BACKUP_SEMANTICS|FILE_FLAG_OPEN_REPARSE_POINT,nullptr));
            require(handle.value!=INVALID_HANDLE_VALUE);BY_HANDLE_FILE_INFORMATION info{};require(GetFileInformationByHandle(handle.value,&info));require((info.dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY)&&!(info.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT));directories.emplace_back(std::move(handle));
            if(end==std::wstring::npos)break;at=end+1;
        }
    }
    static void private_directory(const std::wstring &path){
        HANDLE rawToken=nullptr;require(OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&rawToken));Handle token(rawToken);DWORD needed=0;GetTokenInformation(token.value,TokenUser,nullptr,0,&needed);require(needed>0);
        std::vector<unsigned char> buffer(needed);require(GetTokenInformation(token.value,TokenUser,buffer.data(),needed,&needed));PSID user=reinterpret_cast<TOKEN_USER*>(buffer.data())->User.Sid;
        PSID owner=nullptr;PACL acl=nullptr;PSECURITY_DESCRIPTOR descriptor=nullptr;
        require(GetNamedSecurityInfoW(path.c_str(),SE_FILE_OBJECT,OWNER_SECURITY_INFORMATION|DACL_SECURITY_INFORMATION,&owner,nullptr,&acl,nullptr,&descriptor)==ERROR_SUCCESS);
        struct Free {PSECURITY_DESCRIPTOR p;~Free(){LocalFree(p);}} cleanup{descriptor};
        require(owner&&EqualSid(owner,user)&&acl);SECURITY_DESCRIPTOR_CONTROL control{};DWORD revision=0;require(GetSecurityDescriptorControl(descriptor,&control,&revision)&&(control&SE_DACL_PROTECTED));
        for(DWORD index=0;index<acl->AceCount;++index){void *raw=nullptr;require(GetAce(acl,index,&raw));auto header=reinterpret_cast<ACE_HEADER*>(raw);
            if(header->AceType==ACCESS_DENIED_ACE_TYPE)continue;
            require(header->AceType==ACCESS_ALLOWED_ACE_TYPE);auto ace=reinterpret_cast<ACCESS_ALLOWED_ACE*>(raw);auto sid=reinterpret_cast<PSID>(&ace->SidStart);
            require(EqualSid(sid,user)||IsWellKnownSid(sid,WinLocalSystemSid)||IsWellKnownSid(sid,WinBuiltinAdministratorsSid));
        }
    }
    Handle open(const std::string &name){
        require(safe_path(name));auto suffix=wide(name);std::replace(suffix.begin(),suffix.end(),L'/',L'\\');auto full=root+L"\\"+suffix;
        auto slash=full.find_last_of(L'\\');if(full.substr(0,slash)!=root)lock_parents(full.substr(0,slash));
        Handle h(CreateFileW(full.c_str(),GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,FILE_FLAG_OPEN_REPARSE_POINT|FILE_FLAG_SEQUENTIAL_SCAN,nullptr));require(h.value!=INVALID_HANDLE_VALUE);regular(h.value);return h;
    }
    std::string read(const std::string &name){
        if(name=="result.pdf"){require(result.value!=INVALID_HANDLE_VALUE&&!resultHash.empty());auto bytes=read_file(result.value,MaxPDF);require(sha256(bytes)==resultHash);return bytes;}
        const auto &item=inventory.at(name);auto bytes=read_file(files.at(name).value,item.size);require(bytes.size()==item.size&&sha256(bytes)==item.sha256);return bytes;
    }
    void reserve_result(){
        result=Handle(CreateFileW(output().c_str(),GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL|FILE_FLAG_OPEN_REPARSE_POINT,nullptr));require(result.value!=INVALID_HANDLE_VALUE);regular(result.value);
    }
    void freeze_result(){
        Handle frozen(CreateFileW(output().c_str(),GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,FILE_FLAG_OPEN_REPARSE_POINT,nullptr));require(frozen.value!=INVALID_HANDLE_VALUE);regular(frozen.value);
        auto bytes=read_file(frozen.value,MaxPDF);require(bytes.size()>5&&bytes.rfind("%PDF-",0)==0);resultHash=sha256(bytes);result=std::move(frozen);
    }
    void report(const json &value){
        auto bytes=value.dump();require(bytes.size()<=MaxReport);auto path=root+L"\\result.json";
        Handle h(CreateFileW(path.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL|FILE_FLAG_OPEN_REPARSE_POINT,nullptr));require(h.value!=INVALID_HANDLE_VALUE);
        DWORD written=0;require(WriteFile(h.value,bytes.data(),DWORD(bytes.size()),&written,nullptr)&&written==bytes.size()&&FlushFileBuffers(h.value));
    }
};
std::wstring mime(const std::string &path,const std::string &bytes){
    if(path.rfind("managed/",0)==0){
        if(bytes.rfind("\x89PNG\r\n\x1a\n",0)==0)return L"image/png";
        if(bytes.size()>3&&static_cast<unsigned char>(bytes[0])==255&&static_cast<unsigned char>(bytes[1])==216)return L"image/jpeg";
        if(bytes.rfind("GIF8",0)==0)return L"image/gif";
        if(bytes.size()>12&&bytes.substr(8,4)=="WEBP")return L"image/webp";
        if(bytes.substr(0,1024).find("<svg")!=std::string::npos)return L"image/svg+xml";
        return L"application/octet-stream";
    }
    auto dot=path.rfind('.');auto extension=dot==std::string::npos?"":path.substr(dot);
    const std::map<std::string,std::wstring> types={{".html",L"text/html; charset=utf-8"},{".js",L"text/javascript"},{".mjs",L"text/javascript"},{".css",L"text/css"},{".json",L"application/json"},{".pdf",L"application/pdf"},{".wasm",L"application/wasm"},{".woff2",L"font/woff2"},{".woff",L"font/woff"},{".ttf",L"font/ttf"},{".otf",L"font/otf"},{".svg",L"image/svg+xml"},{".png",L"image/png"},{".jpeg",L"image/jpeg"},{".jpg",L"image/jpeg"},{".gif",L"image/gif"},{".webp",L"image/webp"}};
    auto found=types.find(extension);return found==types.end()?L"application/octet-stream":found->second;
}
void reject_policy_overrides();
class Worker {
    Staging stage;
    std::string mode;
    HWND window=nullptr;
    ComPtr<ICoreWebView2Environment> environment;
    ComPtr<ICoreWebView2Controller> controller;
    ComPtr<ICoreWebView2> view;
    std::wstring expectedNavigation;
    std::string runtimeVersion;
    json warnings=json::array();
    bool navigating=false,loaded=false,polling=false,printing=false,validating=false,finished=false;
    int code=1;
    std::chrono::steady_clock::time_point started=std::chrono::steady_clock::now();
public:
    Worker(const std::wstring &root,const std::string &selected):stage(root,selected),mode(selected){}
    void fail() noexcept {if(finished)return;finished=true;code=1;std::fputs("Document worker failed or capability unavailable\n",stderr);if(controller)controller->Close();PostQuitMessage(1);}
    template<class F> HRESULT guard(F f)noexcept{try{if(!finished)f();return S_OK;}catch(...){fail();return E_FAIL;}}
    void navigate(const wchar_t *entry){loaded=false;expectedNavigation=std::wstring(OriginW)+entry;navigating=true;checked(view->Navigate(expectedNavigation.c_str()));}
    void configure(){
        ComPtr<ICoreWebView2Settings> settings;checked(view->get_Settings(&settings));
        checked(settings->put_AreHostObjectsAllowed(FALSE));checked(settings->put_IsWebMessageEnabled(FALSE));checked(settings->put_AreDevToolsEnabled(FALSE));checked(settings->put_AreDefaultContextMenusEnabled(FALSE));checked(settings->put_AreDefaultScriptDialogsEnabled(FALSE));checked(settings->put_IsStatusBarEnabled(FALSE));checked(settings->put_IsBuiltInErrorPageEnabled(FALSE));
        ComPtr<ICoreWebView2Settings4> settings4;checked(settings.As(&settings4));checked(settings4->put_IsGeneralAutofillEnabled(FALSE));checked(settings4->put_IsPasswordAutosaveEnabled(FALSE));checked(settings4->put_AreBrowserAcceleratorKeysEnabled(FALSE));
        EventRegistrationToken token{};
        checked(view->add_NavigationStarting(Callback<ICoreWebView2NavigationStartingEventHandler>([this](ICoreWebView2*,ICoreWebView2NavigationStartingEventArgs *args){return guard([&]{COMString uri;checked(args->get_Uri(&uri.value));if(!navigating||expectedNavigation!=uri.value){checked(args->put_Cancel(TRUE));return;}navigating=false;});}).Get(),&token));
        checked(view->add_FrameNavigationStarting(Callback<ICoreWebView2NavigationStartingEventHandler>([](ICoreWebView2*,ICoreWebView2NavigationStartingEventArgs *args){return args->put_Cancel(TRUE);}).Get(),&token));
        checked(view->add_NavigationCompleted(Callback<ICoreWebView2NavigationCompletedEventHandler>([this](ICoreWebView2*,ICoreWebView2NavigationCompletedEventArgs *args){return guard([&]{BOOL ok=FALSE;checked(args->get_IsSuccess(&ok));require(ok);loaded=true;});}).Get(),&token));
        checked(view->add_NewWindowRequested(Callback<ICoreWebView2NewWindowRequestedEventHandler>([](ICoreWebView2*,ICoreWebView2NewWindowRequestedEventArgs *args){return args->put_Handled(TRUE);}).Get(),&token));
        checked(view->add_PermissionRequested(Callback<ICoreWebView2PermissionRequestedEventHandler>([](ICoreWebView2*,ICoreWebView2PermissionRequestedEventArgs *args){return args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY);}).Get(),&token));
        checked(view->add_ProcessFailed(Callback<ICoreWebView2ProcessFailedEventHandler>([this](ICoreWebView2*,ICoreWebView2ProcessFailedEventArgs*){fail();return S_OK;}).Get(),&token));
        ComPtr<ICoreWebView2_4> view4;checked(view.As(&view4));
        checked(view4->add_DownloadStarting(Callback<ICoreWebView2DownloadStartingEventHandler>([](ICoreWebView2*,ICoreWebView2DownloadStartingEventArgs *args){args->put_Handled(TRUE);return args->put_Cancel(TRUE);}).Get(),&token));
        ComPtr<ICoreWebView2_22> view22;checked(view.As(&view22));
        checked(view22->AddWebResourceRequestedFilterWithRequestSourceKinds(L"*",COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL,COREWEBVIEW2_WEB_RESOURCE_REQUEST_SOURCE_KINDS_ALL));
        checked(view->add_WebResourceRequested(Callback<ICoreWebView2WebResourceRequestedEventHandler>([this](ICoreWebView2*,ICoreWebView2WebResourceRequestedEventArgs *args){return guard([&]{resource(args);});}).Get(),&token));
        // PrintToPdf is queried before navigation; unsupported engines fail closed.
        if(mode=="render"){ComPtr<ICoreWebView2_7> printer;checked(view.As(&printer));}
        navigate(mode=="render"?L"print.html":L"extract.html");
    }
    void resource(ICoreWebView2WebResourceRequestedEventArgs *args){
        ComPtr<ICoreWebView2WebResourceResponse> denied;checked(environment->CreateWebResourceResponse(nullptr,403,L"Forbidden",L"Cache-Control: no-store\r\n",&denied));checked(args->put_Response(denied.Get()));
        ComPtr<ICoreWebView2WebResourceRequest> request;checked(args->get_Request(&request));COMString uri,method;checked(request->get_Uri(&uri.value));checked(request->get_Method(&method.value));
        std::string path;try{path=request_path(utf8(uri.value,2048),utf8(method.value,16),stage.inventory,validating);}catch(...){return;}
        auto bytes=stage.read(path);require(bytes.size()<=UINT_MAX);
        ComPtr<IStream> stream;stream.Attach(SHCreateMemStream(reinterpret_cast<const BYTE*>(bytes.data()),UINT(bytes.size())));require(stream!=nullptr);
        std::wstring headers=L"Content-Type: "+mime(path,bytes)+L"\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\nReferrer-Policy: no-referrer\r\n";
        headers+=L"Content-Security-Policy: default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; worker-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'\r\n";
        ComPtr<ICoreWebView2WebResourceResponse> response;checked(environment->CreateWebResourceResponse(stream.Get(),200,L"OK",headers.c_str(),&response));checked(args->put_Response(response.Get()));
    }
    void print(){
        printing=true;loaded=false;stage.reserve_result();ComPtr<ICoreWebView2Environment6> env6;checked(environment.As(&env6));ComPtr<ICoreWebView2PrintSettings> settings;checked(env6->CreatePrintSettings(&settings));
        checked(settings->put_PageWidth(595.28/72.0));checked(settings->put_PageHeight(841.89/72.0));checked(settings->put_MarginLeft(36.85/72.0));checked(settings->put_MarginRight(36.85/72.0));checked(settings->put_MarginTop(39.69/72.0));checked(settings->put_MarginBottom(42.52/72.0));checked(settings->put_ShouldPrintBackgrounds(TRUE));checked(settings->put_ShouldPrintHeaderAndFooter(FALSE));
        ComPtr<ICoreWebView2_7> printer;checked(view.As(&printer));
        checked(printer->PrintToPdf(stage.output().c_str(),settings.Get(),Callback<ICoreWebView2PrintToPdfCompletedHandler>([this](HRESULT result,BOOL success){return guard([&]{checked(result);require(success);stage.freeze_result();validating=true;printing=false;navigate(L"validate.html");});}).Get()));
    }
    void tick(){guard([&]{
        require(std::chrono::steady_clock::now()-started<std::chrono::seconds(110));
        if(stage.result.value!=INVALID_HANDLE_VALUE)require(file_size(stage.result.value)<=MaxPDF);
        if(!loaded||polling||printing||!view)return;
        polling=true;const wchar_t *script=mode=="render"&&!validating?L"window.trovePrintStatus ?? null":L"window.troveDocumentStatus ?? null";
        checked(view->ExecuteScript(script,Callback<ICoreWebView2ExecuteScriptCompletedHandler>([this](HRESULT result,LPCWSTR raw){polling=false;return guard([&]{
            checked(result);auto text=utf8(raw,mode=="extract"?MaxStatus:MaxReport);
            if(mode=="render"&&!validating){auto status=print_status(text);if(status.is_null())return;warnings=status["warnings"];print();return;}
            auto status=document_status(text,mode=="extract");if(status.is_null())return;
            if(mode=="extract")write_stdout(status["text"].get_ref<const std::string&>());
            else{stage.report({{"ready",true},{"warnings",warnings},{"physicalPages",status["pages"]},{"runtimeVersion",runtimeVersion}});write_stdout(stage.read("result.pdf"));}
            finished=true;code=0;controller->Close();PostQuitMessage(0);
        });}).Get()));
    });}
    static LRESULT CALLBACK procedure(HWND window,UINT message,WPARAM wp,LPARAM lp){
        if(message==WM_NCCREATE){auto create=reinterpret_cast<CREATESTRUCTW*>(lp);SetWindowLongPtrW(window,GWLP_USERDATA,reinterpret_cast<LONG_PTR>(create->lpCreateParams));}
        auto self=reinterpret_cast<Worker*>(GetWindowLongPtrW(window,GWLP_USERDATA));if(self&&message==WM_TIMER){self->tick();return 0;}
        if(self&&message==WM_CLOSE){self->fail();return 0;}return DefWindowProcW(window,message,wp,lp);
    }
    void shutdown(){finished=true;if(controller)controller->Close();view.Reset();controller.Reset();environment.Reset();}
    int run(){
        WNDCLASSW type{};type.hInstance=GetModuleHandleW(nullptr);type.lpfnWndProc=procedure;type.lpszClassName=L"TrovePrivateDocumentWorker";require(RegisterClassW(&type));
        window=CreateWindowExW(WS_EX_TOOLWINDOW,type.lpszClassName,L"",WS_POPUP,0,0,794,1123,nullptr,nullptr,type.hInstance,this);require(window);require(SetTimer(window,Tick,100,nullptr));
        auto options=Microsoft::WRL::Make<CoreWebView2EnvironmentOptions>();
        checked(options->put_ReleaseChannels(COREWEBVIEW2_RELEASE_CHANNELS_STABLE));
        checked(options->put_TargetCompatibleBrowserVersion(L"141.0.3537.50"));checked(options->put_AllowSingleSignOnUsingOSPrimaryAccount(FALSE));checked(options->put_ExclusiveUserDataFolderAccess(TRUE));checked(options->put_IsCustomCrashReportingEnabled(TRUE));checked(options->put_AreBrowserExtensionsEnabled(FALSE));
        checked(options->put_AdditionalBrowserArguments(L"--disable-background-networking --disable-component-update --no-first-run"));
        checked(CreateCoreWebView2EnvironmentWithOptions(nullptr,stage.profile.c_str(),options.Get(),Callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>([this](HRESULT result,ICoreWebView2Environment *env){return guard([&]{
            checked(result);require(env);environment=env;
            reject_policy_overrides();
            ComPtr<ICoreWebView2Environment7> env7;checked(environment.As(&env7));COMString actualProfile;checked(env7->get_UserDataFolder(&actualProfile.value));require(actualProfile.value&&_wcsicmp(actualProfile.value,stage.profile.c_str())==0);
            COMString version;checked(env->get_BrowserVersionString(&version.value));runtimeVersion=utf8(version.value,256);
            ComPtr<ICoreWebView2Environment10> env10;checked(environment.As(&env10));ComPtr<ICoreWebView2ControllerOptions> controllerOptions;checked(env10->CreateCoreWebView2ControllerOptions(&controllerOptions));checked(controllerOptions->put_IsInPrivateModeEnabled(TRUE));
            checked(env10->CreateCoreWebView2ControllerWithOptions(window,controllerOptions.Get(),Callback<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler>([this](HRESULT result,ICoreWebView2Controller *created){return guard([&]{checked(result);require(created);controller=created;checked(controller->get_CoreWebView2(&view));RECT bounds{0,0,794,1123};checked(controller->put_Bounds(bounds));checked(controller->put_IsVisible(TRUE));configure();});}).Get()));
        });}).Get()));
        MSG message{};while(GetMessageW(&message,nullptr,0,0)>0){TranslateMessage(&message);DispatchMessageW(&message);}
        KillTimer(window,Tick);if(controller)controller->Close();DestroyWindow(window);return code;
    }
};
void reject_policy_overrides(){
    // The documented loader policy can replace explicit folders or append flags.
    // Query only applicable value existence, never read or modify policy contents.
    constexpr wchar_t appID[]=L"local.trove.document.worker";
    checked(SetCurrentProcessExplicitAppUserModelID(appID));
    std::vector<std::wstring> names{appID};
    std::vector<wchar_t> executable(32768);DWORD count=GetModuleFileNameW(nullptr,executable.data(),DWORD(executable.size()));require(count>0&&count<executable.size());
    std::wstring path(executable.data(),count);names.push_back(path.substr(path.find_last_of(L"\\/")+1));
    UINT32 length=0;LONG packageStatus=GetCurrentApplicationUserModelId(&length,nullptr);
    if(packageStatus==ERROR_INSUFFICIENT_BUFFER){require(length>0&&length<=1024);std::vector<wchar_t> packagedID(length);require(GetCurrentApplicationUserModelId(&length,packagedID.data())==ERROR_SUCCESS);names.emplace_back(packagedID.data());}
    else require(packageStatus==APPMODEL_ERROR_NO_APPLICATION||packageStatus==APPMODEL_ERROR_NO_PACKAGE);
    names.emplace_back(L"*");
    for(HKEY hive:{HKEY_LOCAL_MACHINE,HKEY_CURRENT_USER})for(REGSAM view:{KEY_WOW64_64KEY,KEY_WOW64_32KEY}){
        for(const wchar_t *option:{L"BrowserExecutableFolder",L"ChannelSearchKind",L"ReleaseChannels",L"AdditionalBrowserArguments",L"UserDataFolder"}){
            auto keyPath=std::wstring(L"Software\\Policies\\Microsoft\\Edge\\WebView2\\")+option;
            HKEY raw=nullptr;LONG opened=RegOpenKeyExW(hive,keyPath.c_str(),0,KEY_QUERY_VALUE|view,&raw);
            if(opened==ERROR_FILE_NOT_FOUND||opened==ERROR_PATH_NOT_FOUND)continue;
            require(opened==ERROR_SUCCESS);
            struct Key {HKEY key;~Key(){RegCloseKey(key);}} key{raw};
            for(const auto &name:names){if(name==L"*"&&wcscmp(option,L"UserDataFolder")==0)continue;
                DWORD bytes=0;LONG result=RegQueryValueExW(raw,name.c_str(),nullptr,nullptr,nullptr,&bytes);
                require(result==ERROR_FILE_NOT_FOUND);
            }
        }
    }
}
void reject_environment_overrides(){
    LPWCH block=GetEnvironmentStringsW();require(block);bool safe=true;
    for(auto p=block;*p;p+=wcslen(p)+1)if(_wcsnicmp(p,L"WEBVIEW2_",9)==0)safe=false;
    FreeEnvironmentStringsW(block);require(safe);
}
}
int wmain(int argc,wchar_t **argv){
    // Independent hard watchdog also bounds hung COM callbacks/startup/output.
    std::thread([]{Sleep(110000);std::fputs("Document worker deadline exceeded\n",stderr);TerminateProcess(GetCurrentProcess(),1);}).detach();
    try{
        require(argc==3);auto mode=utf8(argv[1],16);require(mode=="render"||mode=="extract");reject_environment_overrides();reject_policy_overrides();
        require(SetDefaultDllDirectories(LOAD_LIBRARY_SEARCH_SYSTEM32));_setmode(_fileno(stdout),_O_BINARY);
        // The supervisor assigns the process-tree Job Object before release.
        unsigned char start=0;DWORD received=0;require(ReadFile(GetStdHandle(STD_INPUT_HANDLE),&start,1,&received,nullptr)&&received==1&&start==1);
        checked(CoInitializeEx(nullptr,COINIT_APARTMENTTHREADED));
        Worker worker(argv[2],mode);int result=worker.run();worker.shutdown();
        CoUninitialize();return result;
    }catch(...){std::fputs("Document worker rejected input or could not start\n",stderr);return 1;}
}
