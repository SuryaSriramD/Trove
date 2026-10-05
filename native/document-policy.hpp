#pragma once
// Platform-neutral validation shared with source-level tests. No browser or IO.
#include "json.hpp"
#include <algorithm>
#include <cstdint>
#include <cctype>
#include <map>
#include <set>
#include <stdexcept>
#include <string>
#include <vector>
namespace trove_document {
using json = nlohmann::json;
constexpr size_t MiB = 1024 * 1024;
constexpr size_t MaxMap = MiB, MaxStatus = 96 * MiB + 65536;
constexpr size_t MaxText = 16 * MiB, MaxReport = 65536, MaxPDF = 256 * MiB;
constexpr const char *Origin = "https://trove-document.invalid/";
inline void require(bool valid) { if (!valid) throw std::runtime_error("document policy rejected input"); }
inline json parse(const std::string &text, size_t maximum) {
    require(!text.empty() && text.size() <= maximum);
    std::vector<std::set<std::string>> objects;
    auto callback = [&](int depth, json::parse_event_t event, json &value) {
        require(depth <= 32);
        if (event == json::parse_event_t::object_start) objects.emplace_back();
        if (event == json::parse_event_t::key) {
            require(!objects.empty());
            require(objects.back().insert(value.get<std::string>()).second);
        }
        if (event == json::parse_event_t::object_end) objects.pop_back();
        return true;
    };
    return json::parse(text, callback);
}
inline bool lower_hex(const std::string &value, size_t size) {
    return value.size() == size && std::all_of(value.begin(),value.end(),[](char c){return (c>='0'&&c<='9')||(c>='a'&&c<='f');});
}
inline bool uuid(const std::string &s) {
    if (s.size()!=36) return false;
    for(size_t i=0;i<s.size();++i) {
        if(i==8||i==13||i==18||i==23) { if(s[i]!='-')return false; }
        else if(!((s[i]>='0'&&s[i]<='9')||(s[i]>='a'&&s[i]<='f')))return false;
    }
    return true;
}
inline bool safe_path(const std::string &s) {
    if(s.empty()||s.size()>512||s.front()=='/'||s.back()=='/')return false;
    for(char c:s)if(!((c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='/'||c=='.'||c=='_'||c=='-'))return false;
    size_t begin=0;
    while(begin<s.size()) {
        auto end=s.find('/',begin); if(end==std::string::npos)end=s.size();
        auto part=s.substr(begin,end-begin); if(part.empty()||part=="."||part==".."||part.back()=='.')return false;
        auto stem=part.substr(0,part.find('.'));for(auto &c:stem)c=static_cast<char>(std::toupper(static_cast<unsigned char>(c)));
        if(stem=="CON"||stem=="PRN"||stem=="AUX"||stem=="NUL"||
           (stem.size()==4&&(stem.rfind("COM",0)==0||stem.rfind("LPT",0)==0)&&stem[3]>='1'&&stem[3]<='9'))return false;
        begin=end+1;
    }
    return true;
}
inline size_t resource_limit(const std::string &path, const std::string &mode) {
    require(safe_path(path));
    if(path=="print.html") {require(mode=="render");return MiB;}
    if(path=="validate.html") {require(mode=="render");return MiB;}
    if(path=="extract.html") {require(mode=="extract");return MiB;}
    if(path=="snapshot.json") {require(mode=="render");return 32*MiB;}
    if(path=="input.pdf") {require(mode=="extract");return 128*MiB;}
    if(path.rfind("managed/",0)==0) {require(mode=="render"&&uuid(path.substr(8)));return 64*MiB;}
    require(path.rfind("assets/",0)==0||path.rfind("pdfjs-assets/",0)==0);
    const std::set<std::string> extensions={".js",".mjs",".css",".json",".woff",".woff2",".ttf",".otf",".pfb",".bcmap",".icc",".wasm",".png",".jpg",".jpeg",".gif",".webp",".svg"};
    auto dot=path.rfind('.');require(dot!=std::string::npos&&extensions.count(path.substr(dot))==1);
    return 64*MiB;
}
struct Resource { std::string sha256; size_t size; };
using Resources = std::map<std::string,Resource>;
inline Resources resources(const std::string &raw,const std::string &mode) {
    require(mode=="render"||mode=="extract");
    auto value=parse(raw,MaxMap);
    require(value.is_object()&&value.size()==2&&value.value("schemaVersion",0)==1&&value.contains("files")&&value["files"].is_object());
    auto files=value["files"];require(files.size()>0&&files.size()<=4096);
    Resources out;std::set<std::string> folded;size_t assets=0,images=0;
    for(auto it=files.begin();it!=files.end();++it) {
        auto limit=resource_limit(it.key(),mode);const auto &record=it.value();
        require(record.is_object()&&record.size()==2&&record.contains("sha256")&&record["sha256"].is_string()&&record.contains("size")&&record["size"].is_number_integer());
        auto size=record["size"].get<int64_t>();auto hash=record["sha256"].get<std::string>();
        require(size>=0&&uint64_t(size)<=limit&&lower_hex(hash,64));
        auto lower=it.key();for(auto &c:lower)c=static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
        require(folded.insert(lower).second);
        if(lower.rfind("managed/",0)==0) {images+=size;require(images<=256*MiB);}
        if(lower.rfind("assets/",0)==0||lower.rfind("pdfjs-assets/",0)==0) {assets+=size;require(assets<=128*MiB);}
        out.emplace(it.key(),Resource{hash,size_t(size)});
    }
    require(out.count(mode=="render"?"print.html":"extract.html")==1);
    require(out.count(mode=="render"?"validate.html":"input.pdf")==1);
    if(mode=="render")require(out.count("snapshot.json")==1);
    return out;
}
inline std::string request_path(const std::string &uri,const std::string &method,const Resources &files,bool validating) {
    require(method=="GET"&&uri.rfind(Origin,0)==0);
    auto path=uri.substr(std::char_traits<char>::length(Origin));require(safe_path(path));
    require(files.count(path)==1||(validating&&path=="result.pdf"));
    return path;
}
inline json ready_status(const std::string &raw, size_t limit) {
    auto value=parse(raw,limit);if(value.is_null())return {};
    require(value.is_object());require(!value.contains("error")||value["error"].is_null());
    if(!value.contains("ready")||value["ready"]==false)return {};
    require(value["ready"]==true);return value;
}
inline json print_status(const std::string &raw) {
    auto value=ready_status(raw,MaxReport);if(value.is_null())return value;
    if(!value.contains("warnings"))value["warnings"]=json::array();
    require(value["warnings"].is_array()&&value["warnings"].size()<=1000);
    for(const auto &warning:value["warnings"])require(warning.is_string()&&warning.get<std::string>().size()<=4096);
    require(value["warnings"].dump().size()<=MaxReport/2);return value;
}
inline json document_status(const std::string &raw,bool extracting) {
    auto value=ready_status(raw,extracting?MaxStatus:MaxReport);if(value.is_null())return value;
    require(value.contains("pages")&&value["pages"].is_number_integer());
    auto pages=value["pages"].get<int64_t>();require(pages>0&&pages<=(extracting?10000:2000));
    if(extracting)require(value.contains("text")&&value["text"].is_string()&&value["text"].get_ref<const std::string &>().size()<=MaxText);
    return value;
}
}
