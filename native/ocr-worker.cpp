// Offline, descriptor-only English OCR. Original images never reach this parser.
#include "worker-platform.hpp"
#include <tesseract/baseapi.h>
#include <tesseract/ocrclass.h>
#include <iostream>
#include <memory>

static std::vector<unsigned char> read_all(int fd,size_t maximum){
 std::vector<unsigned char> out;unsigned char buffer[65536];
 for(;;){auto n=worker_read(fd,buffer,sizeof buffer);if(n<0)throw std::runtime_error("input read");if(!n)break;if(out.size()+size_t(n)>maximum)throw std::runtime_error("input bound");out.insert(out.end(),buffer,buffer+n);}return out;
}
static std::string quote(const std::string &s){
 std::string out="\"";const char *hex="0123456789abcdef";
 for(unsigned char c:s){if(c=='"'||c=='\\'){out+='\\';out+=c;}else if(c<32){out+="\\u00";out+=hex[c>>4];out+=hex[c&15];}else out+=c;}out+='"';return out;
}
static bool deny_file(const char*,std::vector<char>*){return false;}
int main(){try{
 worker_init(2);unsigned char start=0;if(worker_read(0,&start,1)!=1||start!=1)throw std::runtime_error("start barrier");
 auto data=read_all(3,20000012),model=read_all(4,32<<20);
 if(data.size()<12||std::memcmp(data.data(),"TOCR",4))throw std::runtime_error("pixel header");
 uint32_t w,h;std::memcpy(&w,data.data()+4,4);std::memcpy(&h,data.data()+8,4);w=worker_network_u32(w);h=worker_network_u32(h);
 if(!w||!h||uint64_t(w)*h>20000000||data.size()!=12+uint64_t(w)*h)throw std::runtime_error("pixel dimensions");
 worker_digest digest;for(size_t i=0;i<model.size();i+=65536)digest.update(model.data()+i,std::min(size_t(65536),model.size()-i));
 if(digest.finish()!="7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2")throw std::runtime_error("model identity");
 tesseract::TessBaseAPI api;
 if(api.Init(reinterpret_cast<const char*>(model.data()),int(model.size()),"eng",tesseract::OEM_LSTM_ONLY,nullptr,0,nullptr,nullptr,true,deny_file))throw std::runtime_error("model initialization");
 api.SetPageSegMode(tesseract::PSM_AUTO);api.SetImage(data.data()+12,int(w),int(h),1,int(w));
 tesseract::ETEXT_DESC monitor;monitor.set_deadline_msecs(50000);
 if(api.Recognize(&monitor))throw std::runtime_error("recognition deadline");
 std::string output="{\"lines\":[";size_t total=0,count=0;
 std::unique_ptr<tesseract::ResultIterator> it(api.GetIterator());
 if(it){do{std::unique_ptr<char[]> text(it->GetUTF8Text(tesseract::RIL_TEXTLINE));if(!text)continue;
  size_t n=0;while(text[n]&&n<=(1<<20))++n;if(n>(1<<20)||total+n>(1<<20)||++count>10000)throw std::runtime_error("result bound");total+=n;
  if(count>1)output+=",";
  output+="{\"text\":"+quote(std::string(text.get(),n))+",\"confidence\":"+std::to_string(std::clamp(double(it->Confidence(tesseract::RIL_TEXTLINE))/100.,0.,1.))+"}";
 }while(it->Next(tesseract::RIL_TEXTLINE));}
 output+="]}";if(output.size()>2<<20)throw std::runtime_error("JSON bound");
 std::cout<<output;return std::cout.good()?0:1;
}catch(...){std::cerr<<"local OCR failed\n";return 1;}}
