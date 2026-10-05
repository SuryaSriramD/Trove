#pragma once
#include <algorithm>
#include <array>
#include <cerrno>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#include <bcrypt.h>
#include <fcntl.h>
#include <io.h>
#else
#include <unistd.h>
#ifdef __APPLE__
#include <CommonCrypto/CommonDigest.h>
#else
#include <openssl/evp.h>
#endif
#endif

#if defined(__aarch64__) || defined(_M_ARM64)
constexpr const char *WORKER_ARCHITECTURE="arm64";
#elif defined(__x86_64__) || defined(_M_X64)
constexpr const char *WORKER_ARCHITECTURE="amd64";
#else
#error Unsupported native worker architecture
#endif

// Framing uses network byte order without a winsock initialization dependency.
inline uint32_t worker_network_u32(uint32_t value) {
 const uint16_t one=1;
 if(*reinterpret_cast<const unsigned char*>(&one)==0) return value;
 return ((value&0xFF)<<24)|((value&0xFF00)<<8)|((value>>8)&0xFF00)|(value>>24);
}
#ifdef _WIN32
inline std::vector<int>& worker_descriptors(){static std::vector<int> fds{0,1,2};return fds;}
inline void worker_init(){
 _setmode(0,_O_BINARY);_setmode(1,_O_BINARY);
 const char *mapping=std::getenv("TROVE_WORKER_HANDLES");
 if(!mapping||std::strlen(mapping)>1024)throw std::runtime_error("missing worker handle mapping");
 std::string value(mapping);size_t at=0;
 while(at<value.size()){
  auto end=value.find(',',at);if(end==std::string::npos)end=value.size();
  std::string part=value.substr(at,end-at);
  if(part.empty()||part.find_first_not_of("0123456789")!=std::string::npos||worker_descriptors().size()>=36)throw std::runtime_error("invalid worker handle mapping");
  auto number=std::stoull(part);if(!number||number>std::numeric_limits<uintptr_t>::max())throw std::runtime_error("invalid worker handle");
  HANDLE handle=reinterpret_cast<HANDLE>(static_cast<uintptr_t>(number));DWORD flags;
  if(!GetHandleInformation(handle,&flags))throw std::runtime_error("unavailable worker handle");
  // The child never needs to pass handles to another process.
  if(!SetHandleInformation(handle,HANDLE_FLAG_INHERIT,0))throw std::runtime_error("cannot protect worker handle");
  int fd=_open_osfhandle(reinterpret_cast<intptr_t>(handle),_O_BINARY);
  if(fd<0)throw std::runtime_error("cannot map worker handle");
  worker_descriptors().push_back(fd);at=end+1;
 }
 if(worker_descriptors().size()<7||value.back()==',')throw std::runtime_error("incomplete worker handle mapping");
 _putenv_s("TROVE_WORKER_HANDLES","");
}
inline int worker_fd(int slot){return worker_descriptors().at(slot);}
inline int worker_read(int fd,void *buffer,size_t count){return _read(worker_fd(fd),buffer,static_cast<unsigned int>(count));}
inline int worker_write(int fd,const void *buffer,size_t count){return _write(worker_fd(fd),buffer,static_cast<unsigned int>(count));}
inline int worker_read_at(int fd,void *buffer,size_t count,int64_t offset){
 // Each input has a single reading thread. Control pipe reads are separate.
 if(_lseeki64(worker_fd(fd),offset,SEEK_SET)<0)return -1;
 return worker_read(fd,buffer,count);
}
inline void worker_sync(int fd){if(_commit(worker_fd(fd)))throw std::runtime_error("cannot flush result");}
inline FILE *worker_model_file(){return _fdopen(_dup(worker_fd(5)),"rb");}
inline void worker_disable_gpu(){_putenv_s("GGML_METAL_DEVICES","0");}
#else
inline void worker_init(){}
inline int worker_fd(int slot){return slot;}
inline ssize_t worker_read(int fd,void *buffer,size_t count){ssize_t n;do{n=read(fd,buffer,count);}while(n<0&&errno==EINTR);return n;}
inline ssize_t worker_write(int fd,const void *buffer,size_t count){ssize_t n;do{n=write(fd,buffer,count);}while(n<0&&errno==EINTR);return n;}
inline ssize_t worker_read_at(int fd,void *buffer,size_t count,int64_t offset){ssize_t n;do{n=pread(fd,buffer,count,offset);}while(n<0&&errno==EINTR);return n;}
inline void worker_sync(int fd){if(fsync(fd))throw std::runtime_error("cannot flush result");}
inline FILE *worker_model_file(){return fdopen(dup(5),"rb");}
inline void worker_disable_gpu(){setenv("GGML_METAL_DEVICES","0",1);}
#endif

// Use each platform's maintained cryptographic library, not an in-house hash.
class worker_digest {
#ifdef _WIN32
 BCRYPT_ALG_HANDLE algorithm=nullptr;BCRYPT_HASH_HANDLE hash=nullptr;
#elif defined(__APPLE__)
 CC_SHA256_CTX hash;
#else
 EVP_MD_CTX *hash=nullptr;
#endif
public:
 worker_digest(){
#ifdef _WIN32
  if(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,nullptr,0)<0)throw std::runtime_error("SHA256 provider");
  if(BCryptCreateHash(algorithm,&hash,nullptr,0,nullptr,0,0)<0){BCryptCloseAlgorithmProvider(algorithm,0);algorithm=nullptr;throw std::runtime_error("SHA256 initialization");}
#elif defined(__APPLE__)
  CC_SHA256_Init(&hash);
#else
  hash=EVP_MD_CTX_new();if(!hash||EVP_DigestInit_ex(hash,EVP_sha256(),nullptr)!=1){EVP_MD_CTX_free(hash);hash=nullptr;throw std::runtime_error("SHA256 initialization");}
#endif
 }
 worker_digest(const worker_digest&)=delete;
 worker_digest& operator=(const worker_digest&)=delete;
 ~worker_digest(){
#ifdef _WIN32
  if(hash)BCryptDestroyHash(hash);if(algorithm)BCryptCloseAlgorithmProvider(algorithm,0);
#elif !defined(__APPLE__)
  EVP_MD_CTX_free(hash);
#endif
 }
 void update(const void *data,size_t count){
  // All callers supply at most one 64 KiB chunk or a bounded result.
  if(count>UINT32_MAX)throw std::runtime_error("SHA256 chunk limit");
#ifdef _WIN32
  if(BCryptHashData(hash,(PUCHAR)data,(ULONG)count,0)<0)throw std::runtime_error("SHA256 update");
#elif defined(__APPLE__)
  CC_SHA256_Update(&hash,data,(CC_LONG)count);
#else
  if(EVP_DigestUpdate(hash,data,count)!=1)throw std::runtime_error("SHA256 update");
#endif
 }
 std::string finish(){
  unsigned char digest[32];
#ifdef _WIN32
  if(BCryptFinishHash(hash,digest,sizeof(digest),0)<0)throw std::runtime_error("SHA256 finish");
#elif defined(__APPLE__)
  CC_SHA256_Final(digest,&hash);
#else
  unsigned length=0;if(EVP_DigestFinal_ex(hash,digest,&length)!=1||length!=32)throw std::runtime_error("SHA256 finish");
#endif
  char hex[65];for(int i=0;i<32;i++)snprintf(hex+i*2,3,"%02x",digest[i]);return hex;
 }
};
inline std::string worker_sha256(const void *data,size_t count){worker_digest hash;hash.update(data,count);return hash.finish();}
