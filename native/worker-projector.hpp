#pragma once
#include "worker.hpp"

// mtmd at the locked commit only accepts a filename. POSIX resolves the leased
// descriptor directly. Windows stages bounded, rehashed bytes in the parent's
// owner-only directory; CREATE_NEW rejects existing files and reparse points.
class worker_projector {
#ifdef _WIN32
 HANDLE file=INVALID_HANDLE_VALUE;
#endif
public:
 worker_projector(){
#ifdef _WIN32
  const auto &expected=request.at("inputs").at(3);
  int64_t size=expected.at("sizeBytes");
  if(size<4||size>(512LL<<20))throw std::runtime_error("projector size limit");
  file=CreateFileW(L"projector.gguf",GENERIC_READ|GENERIC_WRITE,FILE_SHARE_READ,nullptr,CREATE_NEW,FILE_ATTRIBUTE_TEMPORARY|FILE_FLAG_OPEN_REPARSE_POINT,nullptr);
  if(file==INVALID_HANDLE_VALUE)throw std::runtime_error("cannot create private projector");
  try{
   worker_digest hash;std::array<char,65536> buffer;
   for(int64_t at=0;at<size;){check_cancel();auto n=worker_read_at(8,buffer.data(),static_cast<size_t>(std::min<int64_t>(buffer.size(),size-at)),at);if(n<=0)throw std::runtime_error("projector truncated");hash.update(buffer.data(),n);DWORD written=0;if(!WriteFile(file,buffer.data(),static_cast<DWORD>(n),&written,nullptr)||written!=n)throw std::runtime_error("projector staging failed");at+=n;}
   if(hash.finish()!=expected.at("sha256").get<std::string>())throw std::runtime_error("projector digest mismatch");
   if(!FlushFileBuffers(file))throw std::runtime_error("projector flush failed");
  }catch(...){CloseHandle(file);file=INVALID_HANDLE_VALUE;DeleteFileW(L"projector.gguf");throw;}
#endif
 }
 worker_projector(const worker_projector&)=delete;
 ~worker_projector(){
#ifdef _WIN32
  if(file!=INVALID_HANDLE_VALUE)CloseHandle(file);DeleteFileW(L"projector.gguf");
#endif
 }
 const char *path()const{
#ifdef _WIN32
  return "projector.gguf";
#elif defined(__linux__)
  return "/proc/self/fd/8";
#else
  return "/dev/fd/8";
#endif
 }
};
