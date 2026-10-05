// Isolated WebKitGTK 2.52.6 PDF helper; no workspace or app bridge.
#include "document-policy.hpp"
#include <gtk/gtk.h>
#include <gtk/gtkunixprint.h>
#include <webkit2/webkit2.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>
#include <cerrno>
#include <cstring>
#include <fstream>
#include <iostream>
#include <memory>

namespace td=trove_document;
namespace {
constexpr const char *Origin="trove-document://document/";
struct FD {int value=-1;explicit FD(int v=-1):value(v){}~FD(){if(value>=0)close(value);}FD(const FD&)=delete;};
int rootFD=-1;
std::string mode,root,phase,outputText,generatedHash;
td::Resources resources;
td::json warnings=td::json::array();
bool failed=false,finished=false,polling=false,policyReady=false;
GtkWidget *window=nullptr;
WebKitWebView *view=nullptr;
WebKitWebContext *webContext=nullptr;

void fail(){failed=true;gtk_main_quit();}
std::string hash(const std::string &data){gchar *raw=g_compute_checksum_for_data(G_CHECKSUM_SHA256,reinterpret_cast<const guchar*>(data.data()),data.size());td::require(raw);std::string result(raw);g_free(raw);return result;}
int open_relative(const std::string &name){
 td::require(td::safe_path(name));FD current(dup(rootFD));size_t begin=0;
 for(;;){auto slash=name.find('/',begin);bool final=slash==std::string::npos;auto component=name.substr(begin,final?std::string::npos:slash-begin);
  int fd=openat(current.value,component.c_str(),O_RDONLY|O_CLOEXEC|O_NOFOLLOW|O_NONBLOCK|(final?0:O_DIRECTORY));td::require(fd>=0);
  struct stat st{};if(fstat(fd,&st)!=0||st.st_uid!=getuid()||(final?(!S_ISREG(st.st_mode)||st.st_nlink!=1):!S_ISDIR(st.st_mode))){close(fd);throw std::runtime_error("resource type");}
  if(final)return fd;close(current.value);current.value=fd;begin=slash+1;
 }
}
std::string read_file(const std::string &name,size_t maximum){
 FD file(open_relative(name));struct stat st{};td::require(fstat(file.value,&st)==0&&st.st_size>=0&&uint64_t(st.st_size)<=maximum);
 std::string data;data.reserve(size_t(st.st_size));char buffer[65536];
 for(;;){ssize_t n=read(file.value,buffer,sizeof(buffer));if(n<0&&errno==EINTR)continue;td::require(n>=0);if(!n)break;td::require(data.size()+size_t(n)<=maximum);data.append(buffer,size_t(n));}
 td::require(data.size()==size_t(st.st_size));return data;
}
void write_file(const std::string &name,const std::string &bytes){
 td::require(td::safe_path(name)&&name.find('/')==std::string::npos);FD file(openat(rootFD,name.c_str(),O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600));td::require(file.value>=0);
 size_t offset=0;while(offset<bytes.size()){ssize_t n=write(file.value,bytes.data()+offset,bytes.size()-offset);if(n<0&&errno==EINTR)continue;td::require(n>0);offset+=size_t(n);}td::require(fsync(file.value)==0);
}
std::string read_resource(const std::string &name){
 if(name=="result.pdf"){td::require(phase=="validate"&&!generatedHash.empty());auto data=read_file(name,td::MaxPDF);td::require(hash(data)==generatedHash);return data;}
 auto found=resources.find(name);td::require(found!=resources.end());auto data=read_file(name,found->second.size);td::require(data.size()==found->second.size&&hash(data)==found->second.sha256);return data;
}
std::string path(const char *uri,const char *method){
 td::require(uri&&method);std::string u(uri);td::require(u.rfind(Origin,0)==0);
 return td::request_path(std::string(td::Origin)+u.substr(std::strlen(Origin)),method,resources,phase=="validate");
}
const char *mime(const std::string &name,const std::string &bytes){
 auto dot=name.rfind('.');std::string ext=dot==std::string::npos?"":name.substr(dot);
 if(ext==".html")return "text/html; charset=utf-8";if(ext==".js"||ext==".mjs")return "text/javascript; charset=utf-8";if(ext==".css")return "text/css; charset=utf-8";
 if(ext==".json")return "application/json";if(ext==".wasm")return "application/wasm";if(ext==".pdf")return "application/pdf";if(ext==".svg")return "image/svg+xml";
 if(ext==".woff")return "font/woff";if(ext==".woff2")return "font/woff2";if(ext==".ttf")return "font/ttf";if(ext==".otf")return "font/otf";
 if(bytes.size()>=8&&bytes.compare(0,8,"\x89PNG\r\n\x1a\n",8)==0)return "image/png";
 if(bytes.size()>=3&&static_cast<unsigned char>(bytes[0])==255&&static_cast<unsigned char>(bytes[1])==216&&static_cast<unsigned char>(bytes[2])==255)return "image/jpeg";
 if(bytes.rfind("GIF87a",0)==0||bytes.rfind("GIF89a",0)==0)return "image/gif";
 if(bytes.size()>=12&&bytes.rfind("RIFF",0)==0&&bytes.compare(8,4,"WEBP")==0)return "image/webp";
 return "application/octet-stream";
}
void scheme_request(WebKitURISchemeRequest *request,gpointer){try{
 auto name=path(webkit_uri_scheme_request_get_uri(request),webkit_uri_scheme_request_get_http_method(request));auto data=read_resource(name);
 GBytes *bytes=g_bytes_new(data.data(),data.size());GInputStream *stream=g_memory_input_stream_new_from_bytes(bytes);g_bytes_unref(bytes);
 auto response=webkit_uri_scheme_response_new(stream,data.size());webkit_uri_scheme_response_set_content_type(response,mime(name,data));
 SoupMessageHeaders *headers=soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);soup_message_headers_append(headers,"X-Content-Type-Options","nosniff");soup_message_headers_append(headers,"Cache-Control","no-store");soup_message_headers_append(headers,"Content-Security-Policy","default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'");webkit_uri_scheme_response_set_http_headers(response,headers); // response owns headers
 webkit_uri_scheme_request_finish_with_response(request,response);g_object_unref(response);g_object_unref(stream);
 }catch(...){GError *error=g_error_new_literal(G_IO_ERROR,G_IO_ERROR_PERMISSION_DENIED,"Document resource unavailable");webkit_uri_scheme_request_finish_error(request,error);g_error_free(error);fail();}}
gboolean policy(WebKitWebView*,WebKitPolicyDecision *decision,WebKitPolicyDecisionType type,gpointer){
 if(type==WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION){webkit_policy_decision_ignore(decision);return TRUE;}
 if(type!=WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION)return FALSE;
 auto action=webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(decision));auto request=webkit_navigation_action_get_request(action);
 if(phase=="starting"&&std::strcmp(webkit_uri_request_get_uri(request),"about:blank")==0)return FALSE;
 auto expected=std::string(Origin)+(phase=="print"?"print.html":phase=="extract"?"extract.html":"validate.html");
 if(webkit_uri_request_get_uri(request)!=expected){webkit_policy_decision_ignore(decision);return TRUE;}return FALSE;
}
gboolean permission(WebKitWebView*,WebKitPermissionRequest *request,gpointer){webkit_permission_request_deny(request);return TRUE;}
GtkWidget *new_window(WebKitWebView*,WebKitNavigationAction*,gpointer){return nullptr;}
void download(WebKitWebContext*,WebKitDownload *item,gpointer){webkit_download_cancel(item);}
gboolean load_failed(WebKitWebView*,WebKitLoadEvent,const gchar*,GError*,gpointer){fail();return TRUE;}
void print_failed(WebKitPrintOperation*,GError*,gpointer){fail();}
void print_finished(WebKitPrintOperation *operation,gpointer){g_object_unref(operation);if(failed)return;try{
 auto pdf=read_file("result.pdf",td::MaxPDF);td::require(pdf.size()>=5&&pdf.rfind("%PDF-",0)==0);generatedHash=hash(pdf);phase="validate";
 webkit_web_view_load_uri(view,(std::string(Origin)+"validate.html").c_str());
 }catch(...){fail();}}
gboolean find_file_printer(GtkPrinter *printer,gpointer data){
 if(gtk_printer_is_virtual(printer)&&gtk_printer_accepts_pdf(printer)&&std::strcmp(gtk_printer_get_name(printer),"Print to File")==0){*static_cast<bool*>(data)=true;return TRUE;}return FALSE;
}
void print(){
 bool available=false;gtk_enumerate_printers(find_file_printer,&available,nullptr,TRUE);td::require(available);
 struct stat st{};td::require(fstatat(rootFD,"result.pdf",&st,AT_SYMLINK_NOFOLLOW)!=0&&errno==ENOENT);
 auto settings=gtk_print_settings_new();gtk_print_settings_set_printer(settings,"Print to File");gtk_print_settings_set(settings,GTK_PRINT_SETTINGS_OUTPUT_FILE_FORMAT,"pdf");
 gchar *uri=g_filename_to_uri((root+"/result.pdf").c_str(),nullptr,nullptr);td::require(uri);gtk_print_settings_set(settings,GTK_PRINT_SETTINGS_OUTPUT_URI,uri);g_free(uri);
 auto setup=gtk_page_setup_new();auto paper=gtk_paper_size_new(GTK_PAPER_NAME_A4);gtk_page_setup_set_paper_size(setup,paper);gtk_paper_size_free(paper);
 gtk_page_setup_set_top_margin(setup,36,GTK_UNIT_POINTS);gtk_page_setup_set_bottom_margin(setup,36,GTK_UNIT_POINTS);gtk_page_setup_set_left_margin(setup,36,GTK_UNIT_POINTS);gtk_page_setup_set_right_margin(setup,36,GTK_UNIT_POINTS);
 auto operation=webkit_print_operation_new(view);webkit_print_operation_set_print_settings(operation,settings);webkit_print_operation_set_page_setup(operation,setup);g_object_unref(settings);g_object_unref(setup);
 g_signal_connect(operation,"failed",G_CALLBACK(print_failed),nullptr);g_signal_connect(operation,"finished",G_CALLBACK(print_finished),nullptr);phase="printing";webkit_print_operation_print(operation);
}
void evaluated(GObject *object,GAsyncResult *res,gpointer){polling=false;if(failed||finished)return;GError *error=nullptr;JSCValue *value=webkit_web_view_evaluate_javascript_finish(WEBKIT_WEB_VIEW(object),res,&error);
 if(error){g_error_free(error);if(value)g_object_unref(value);fail();return;}try{
 td::require(value&&jsc_value_is_string(value));gchar *raw=jsc_value_to_string(value);g_object_unref(value);value=nullptr;td::require(raw);std::string result(raw);g_free(raw);
 if(phase=="print"){auto status=td::print_status(result);if(status.is_null())return;warnings=status["warnings"];print();return;}
 if(phase!="extract"&&phase!="validate")return;
 auto status=td::document_status(result,phase=="extract");if(status.is_null())return;
 if(phase=="extract")outputText=status["text"].get<std::string>();
 else{auto report=td::json{{"ready",true},{"warnings",warnings},{"physicalPages",status["pages"]},{"runtimeVersion","WebKitGTK 2.52.6"}}.dump();td::require(report.size()<=td::MaxReport);write_file("result.json",report);}
 finished=true;gtk_main_quit();
 }catch(...){if(value)g_object_unref(value);fail();}}
gboolean tick(gpointer){if(failed||finished)return G_SOURCE_REMOVE;try{
 struct stat st{};if(fstatat(rootFD,"result.pdf",&st,AT_SYMLINK_NOFOLLOW)==0)td::require(S_ISREG(st.st_mode)&&st.st_nlink==1&&st.st_size>=0&&uint64_t(st.st_size)<=td::MaxPDF);
 if(!policyReady||polling||phase=="printing")return G_SOURCE_CONTINUE;
 polling=true;const char *script=phase=="print"?"JSON.stringify(window.trovePrintStatus || null)":"JSON.stringify(window.troveDocumentStatus || null)";
 webkit_web_view_evaluate_javascript(view,script,-1,nullptr,nullptr,nullptr,evaluated,nullptr);return G_SOURCE_CONTINUE;
 }catch(...){fail();return G_SOURCE_REMOVE;}}
gboolean policy_ready(WebKitWebView*,WebKitUserMessage *message,gpointer){
 if(std::strcmp(webkit_user_message_get_name(message),"trove-document-policy-ready")!=0||phase!="starting")return FALSE;
 policyReady=true;phase=mode=="render"?"print":"extract";webkit_web_view_load_uri(view,(std::string(Origin)+(mode=="render"?"print.html":"extract.html")).c_str());return TRUE;
}
gboolean timeout(gpointer){fail();return G_SOURCE_REMOVE;}
}
int main(int argc,char **argv){try{
 char start=0;td::require(std::cin.get(start).good()&&start==1);
 td::require(argc==3);mode=argv[1];root=argv[2];td::require(mode=="render"||mode=="extract");td::require(!root.empty()&&root.front()=='/'&&root.back()!='/');
 char *canonical=realpath(root.c_str(),nullptr);td::require(canonical);bool canonicalPath=root==canonical;free(canonical);td::require(canonicalPath);
 umask(0077);rootFD=open(root.c_str(),O_RDONLY|O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW);td::require(rootFD>=0);struct stat st{};td::require(fstat(rootFD,&st)==0&&st.st_uid==getuid()&&(st.st_mode&0077)==0);
 resources=td::resources(read_file("resource-map.json",td::MaxMap),mode);for(const auto &entry:resources)read_resource(entry.first);
 td::require(webkit_get_major_version()==2&&webkit_get_minor_version()==52&&webkit_get_micro_version()==6);
 setenv("GTK_PRINT_BACKENDS","file",1);setenv("LC_ALL","C",1);td::require(gtk_init_check(nullptr,nullptr));
 auto manager=webkit_website_data_manager_new_ephemeral();webContext=webkit_web_context_new_with_website_data_manager(manager);g_object_unref(manager);webkit_web_context_set_sandbox_enabled(webContext,TRUE);
 auto extensions=root.substr(0,root.rfind('/'))+"/web-extensions";
 webkit_web_context_set_web_extensions_directory(webContext,extensions.c_str());
 auto extensionData=td::json{{"mode",mode},{"resources",read_file("resource-map.json",td::MaxMap)}}.dump();
 webkit_web_context_set_web_extensions_initialization_user_data(webContext,g_variant_new_string(extensionData.c_str()));
 auto security=webkit_web_context_get_security_manager(webContext);webkit_security_manager_register_uri_scheme_as_secure(security,"trove-document");webkit_security_manager_register_uri_scheme_as_cors_enabled(security,"trove-document");
 webkit_web_context_register_uri_scheme(webContext,"trove-document",scheme_request,nullptr,nullptr);g_signal_connect(webContext,"download-started",G_CALLBACK(download),nullptr);
 window=gtk_offscreen_window_new();view=WEBKIT_WEB_VIEW(webkit_web_view_new_with_context(webContext));gtk_container_add(GTK_CONTAINER(window),GTK_WIDGET(view));gtk_widget_set_size_request(GTK_WIDGET(view),1000,1400);
 auto settings=webkit_web_view_get_settings(view);webkit_settings_set_enable_developer_extras(settings,FALSE);webkit_settings_set_enable_html5_database(settings,FALSE);webkit_settings_set_enable_html5_local_storage(settings,FALSE);webkit_settings_set_enable_write_console_messages_to_stdout(settings,FALSE);webkit_settings_set_print_backgrounds(settings,TRUE);
 g_signal_connect(view,"decide-policy",G_CALLBACK(policy),nullptr);g_signal_connect(view,"permission-request",G_CALLBACK(permission),nullptr);g_signal_connect(view,"create",G_CALLBACK(new_window),nullptr);g_signal_connect(view,"load-failed",G_CALLBACK(load_failed),nullptr);
 g_signal_connect(view,"user-message-received",G_CALLBACK(policy_ready),nullptr);phase="starting";webkit_web_view_load_uri(view,"about:blank");gtk_widget_show_all(window);
 g_timeout_add(100,tick,nullptr);g_timeout_add_seconds(110,timeout,nullptr);gtk_main();gtk_widget_destroy(window);g_object_unref(webContext);td::require(!failed&&finished);
 if(mode=="render")outputText=read_resource("result.pdf");std::cout.write(outputText.data(),outputText.size());td::require(std::cout.good());close(rootFD);return 0;
 }catch(...){std::cerr<<"Local document processing failed or required capability is unavailable\n";if(rootFD>=0)close(rootFD);return 1;}}
