#pragma once
#include "json.hpp"
#include <CommonCrypto/CommonDigest.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <atomic>
#include <thread>
#include <vector>
#include <string>
#include <stdexcept>
#include <cmath>
using json=nlohmann::json;
constexpr uint32_t MAX_FRAME=1048576;
static std::atomic<bool> cancelled(false);
static json request;
static uint64_t sequence=0;
inline void read_all(int fd,void*data,size_t n){char*p=(char*)data;while(n){ssize_t k=read(fd,p,std::min(n,size_t(65536)));if(k<=0)throw std::runtime_error("partial pipe frame");p+=k;n-=k;}}
inline void write_all(int fd,const void*data,size_t n){const char*p=(const char*)data;while(n){ssize_t k=write(fd,p,std::min(n,size_t(65536)));if(k<=0)throw std::runtime_error("pipe write failed");p+=k;n-=k;}}
inline json read_frame(int fd){uint32_t n;read_all(fd,&n,4);n=ntohl(n);if(!n||n>MAX_FRAME)throw std::runtime_error("frame length");std::string b(n,'\0');read_all(fd,b.data(),n);return json::parse(b);}
inline void frame(json value){std::string b=value.dump();if(b.size()>MAX_FRAME)throw std::runtime_error("output frame length");uint32_t n=htonl((uint32_t)b.size());write_all(1,&n,4);write_all(1,b.data(),b.size());}
inline void event(json value){value["requestId"]=request.at("requestId");value["sequence"]=sequence++;frame(value);}
inline void progress(int completed,const char*message){event({{"kind","progress"},{"progress",{{"sequence",sequence},{"completed",completed},{"unit","steps"},{"message",message}}}});}
inline std::string read_input(int index,size_t maximum){auto h=request.at("inputs").at(index);size_t size=h.at("sizeBytes");if(size>maximum)throw std::runtime_error("input too large");std::string b(size,'\0');size_t at=0;while(at<size){auto n=pread(5+index,b.data()+at,std::min(size-at,size_t(65536)),at);if(n<=0)throw std::runtime_error("input truncated");at+=n;}return b;}
inline void init_control(){std::thread([]{try{auto c=read_frame(3);if(c.at("protocolVersion")==1&&c.at("kind")=="cancel"&&c.at("requestId")==request.at("requestId")&&c.at("cancellationId")==request.at("cancellationId"))cancelled=true;}catch(...){cancelled=true;}}).detach();}
inline void check_cancel(){if(cancelled)throw std::runtime_error("cancelled");}
inline bool abort_work(void*){return cancelled.load();}
inline void finish(json value){check_cancel();std::string b=value.dump();if(b.size()>request.at("limits").at("maxOutputBytes").get<size_t>())throw std::runtime_error("result too large");write_all(4,b.data(),b.size());fsync(4);unsigned char hash[CC_SHA256_DIGEST_LENGTH];CC_SHA256(b.data(),(CC_LONG)b.size(),hash);char hex[65];for(int i=0;i<32;i++)snprintf(hex+i*2,3,"%02x",hash[i]);event({{"kind","result"},{"leaseToken",request.at("leaseToken")},{"resultHandle",{{"handleId",request.at("requestId")},{"kind","result"},{"sha256",std::string(hex)},{"sizeBytes",b.size()}}}});}
inline int failure(const std::exception&e){try{if(!request.is_null()){if(cancelled){event({{"kind","cancelled"}});}else{event({{"kind","error"},{"code","RUNTIME_FAILURE"},{"message",std::string(e.what()).substr(0,2048)},{"retryable",false}});}}}catch(...){}return 1;}
