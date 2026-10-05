#include "document-policy.hpp"
#include <cassert>
#include <iostream>
using namespace trove_document;
template<class F> void rejected(F f){bool failed=false;try{f();}catch(...){failed=true;}assert(failed);}
json record(size_t size=1){return {{"size",size},{"sha256",std::string(64,'a')}};}
json source_map(bool render=true){json files;if(render)files={{"print.html",record()},{"validate.html",record()},{"snapshot.json",record()}};else files={{"extract.html",record()},{"input.pdf",record()}};return {{"schemaVersion",1},{"files",files}};}
int main(){
    auto good=source_map();auto files=resources(good.dump(),"render");assert(files.size()==3);
    assert(request_path(std::string(Origin)+"print.html","GET",files,false)=="print.html");
    assert(request_path(std::string(Origin)+"result.pdf","GET",files,true)=="result.pdf");
    rejected([&]{request_path(std::string(Origin)+"result.pdf","GET",files,false);});
    for(const auto &uri:{"file:///print.html","https://example.com/print.html","http://trove-document.invalid/print.html","https://trove-document.invalid:443/print.html","https://name@trove-document.invalid/print.html","https://trove-document.invalid/print.html?x=1","https://trove-document.invalid/print.html#x","https://trove-document.invalid/%2e%2e/print.html","https://trove-document.invalid/../print.html","https://trove-document.invalid//print.html","https://trove-document.invalid/resource-map.json","https://trove-document.invalid/result.json","https://trove-document.invalid/webview-profile/Cookies"})rejected([&]{request_path(uri,"GET",files,false);});
    rejected([&]{request_path(std::string(Origin)+"print.html","POST",files,false);});
    for(const auto &name:{"assets/../outside.js","assets/x/../../private.js","assets/a\\b.js","assets/CON.js","assets/aux/x.js","assets/file.js:stream","assets/a./x.js","assets/test.html","managed/not-a-uuid","input.pdf","result.pdf","resource-map.json"}){auto bad=good;bad["files"][name]=record();rejected([&]{resources(bad.dump(),"render");});}
    auto cases=good;cases["files"]["assets/A.js"]=record();cases["files"]["assets/a.js"]=record();rejected([&]{resources(cases.dump(),"render");});
    auto malformed=good;malformed["files"]["print.html"]["sha256"]=std::string(64,'A');rejected([&]{resources(malformed.dump(),"render");});
    malformed=good;malformed["files"]["print.html"]["size"]=-1;rejected([&]{resources(malformed.dump(),"render");});
    malformed=good;malformed["files"]["snapshot.json"]["size"]=32*MiB+1;rejected([&]{resources(malformed.dump(),"render");});
    malformed=good;malformed["files"].erase("validate.html");rejected([&]{resources(malformed.dump(),"render");});
    rejected([&]{resources(R"({"schemaVersion":1,"schemaVersion":1,"files":{}})","render");});
    rejected([&]{parse(R"({"outer":{"name":1,"name":2}})",1024);});
    assert(parse(R"({"outer":[{"name":1},{"name":2}]})",1024)["outer"].size()==2);
    rejected([&]{parse(std::string(40,'[')+"0"+std::string(40,']'),1024);});
    auto aggregate=good;for(int i=0;i<3;++i)aggregate["files"]["assets/part"+std::to_string(i)+".js"]=record(64*MiB);rejected([&]{resources(aggregate.dump(),"render");});
    auto extract=source_map(false);extract["files"]["input.pdf"]["size"]=128*MiB;assert(resources(extract.dump(),"extract").size()==2);
    extract["files"]["input.pdf"]["size"]=128*MiB+1;rejected([&]{resources(extract.dump(),"extract");});
    assert(print_status("null").is_null());assert(print_status(R"({"ready":false,"warnings":[]})").is_null());
    auto status=print_status(R"({"ready":true,"warnings":["Source image missing"]})");assert(status["warnings"].at(0)=="Source image missing");
    rejected([&]{print_status(R"({"ready":true,"error":"failed","warnings":[]})");});
    rejected([&]{print_status(R"({"ready":true,"warnings":[{"code":"bad"}]})");});
    rejected([&]{print_status(std::string(MaxReport+1,' '));});
    assert(document_status(R"({"ready":false})",true).is_null());
    auto text=document_status(R"({"ready":true,"pages":2,"text":"first\fsecond"})",true);assert(text["text"]=="first\fsecond");
    for(int pages:{0,-1,2001})rejected([&]{document_status(json({{"ready",true},{"pages",pages}}).dump(),false);});
    assert(document_status(R"({"ready":true,"pages":2000})",false)["pages"]==2000);
    rejected([&]{document_status(R"({"ready":true,"pages":10001,"text":"x"})",true);});
    rejected([&]{document_status(R"({"ready":true,"pages":1.5,"text":"x"})",true);});
    rejected([&]{document_status(json({{"ready",true},{"pages",1},{"text",std::string(MaxText+1,'x')}}).dump(),true);});
    // Legitimate control-character escaping must not reduce the UTF8 text limit.
    std::string control(128*1024,'\0');assert(document_status(json({{"ready",true},{"pages",1},{"text",control}}).dump(),true)["text"]==control);
    std::cout<<"document resource, origin, bounds, duplicate-key, and status policy checks passed\n";
}
