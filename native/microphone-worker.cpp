#include "worker-platform.hpp"
#include "capture-buffer.hpp"
#include <nlohmann/json.hpp>
#include <chrono>
#include <thread>
#include <sys/stat.h>
#ifndef _WIN32
#include <poll.h>
#endif

#define MINIAUDIO_IMPLEMENTATION
#define MA_NO_DECODING
#define MA_NO_ENCODING
#define MA_NO_RESOURCE_MANAGER
#define MA_NO_NODE_GRAPH
#define MA_NO_ENGINE
#define MA_NO_GENERATION
#ifndef _WIN32
#define MA_NO_RUNTIME_LINKING
#else
// miniaudio's Windows desktop COM setup requires dynamic loading. Restrict its
// only LoadLibraryA call to named OS libraries, never the app or current folder.
static HMODULE capture_system_library(const char *name) {
 const char *allowed[]={"kernel32.dll","user32.dll","advapi32.dll","ole32.dll","avrt.dll","mmdevapi.dll"};
 for(const char *library:allowed)if(std::strcmp(name,library)==0)return LoadLibraryExA(name,nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32);
 SetLastError(ERROR_ACCESS_DENIED);return nullptr;
}
#define LoadLibraryA capture_system_library
#endif
#define MA_ENABLE_ONLY_SPECIFIC_BACKENDS
#if defined(TROVE_CAPTURE_FIXTURE_ONLY)
#define MA_NO_DEVICE_IO
#elif defined(_WIN32)
#define MA_ENABLE_WASAPI
#elif defined(__linux__)
#define MA_ENABLE_PULSEAUDIO
#else
#error The portable microphone device backend is only enabled on Windows/Linux.
#endif
#include <miniaudio/miniaudio.h>
#ifdef _WIN32
#undef LoadLibraryA
#endif

using json=nlohmann::json;
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
constexpr const char *CAPTURE_COMMIT="miniaudio-0.11.25-trove-microphone-v1-fixture";
#else
constexpr const char *CAPTURE_COMMIT="miniaudio-0.11.25-trove-microphone-v1";
#endif
static CaptureBuffer buffer;
static std::atomic<bool> cancelled{false};
static std::string request_id;
static int64_t sequence=0,bytes=0;
static bool synthetic=false;
static const char *backend="not_started",*permission="not_requested",*device_state="not_accessed";
struct CaptureFailure:std::runtime_error{
 const char *code;
 CaptureFailure(const char *code,const char *message):std::runtime_error(message),code(code){}
};
void write_all(int slot,const void *data,size_t size){
 auto *p=static_cast<const char*>(data);
 while(size){auto n=worker_write(slot,p,std::min(size,size_t(65536)));if(n<=0)throw CaptureFailure("IO_ERROR","capture output write failed");p+=n;size-=size_t(n);}
}
bool pipe_ready(int slot,std::atomic<bool> *stop) {
 while(!stop->load()) {
#ifdef _WIN32
  DWORD available=0;
  if(!PeekNamedPipe(reinterpret_cast<HANDLE>(_get_osfhandle(worker_fd(slot))),nullptr,0,nullptr,&available,nullptr))return true;
  if(available)return true;
  std::this_thread::sleep_for(std::chrono::milliseconds(10));
#else
  pollfd descriptor{worker_fd(slot),POLLIN,0};
  int status=poll(&descriptor,1,10);
  if(status>0)return true;
  if(status<0&&errno!=EINTR)return true;
#endif
 }
 return false;
}
json read_frame(int slot,std::atomic<bool> *stop=nullptr){
 auto read_all=[slot,stop](void *data,size_t size){char *p=static_cast<char*>(data);while(size){if(stop&&!pipe_ready(slot,stop))throw CaptureFailure("CANCELLED","control reader stopped");auto n=worker_read(slot,p,size);if(n<=0)throw CaptureFailure("INVALID_INPUT","incomplete capture frame");p+=n;size-=size_t(n);}};
 uint32_t size;read_all(&size,4);size=worker_network_u32(size);
 if(size==0||size>4096)throw CaptureFailure("INVALID_INPUT","capture frame exceeds 4096 bytes");
 std::string data(size,'\0');read_all(data.data(),size);return json::parse(data);
}
void frame(json value){auto text=value.dump();uint32_t size=worker_network_u32(uint32_t(text.size()));write_all(1,&size,4);write_all(1,text.data(),text.size());}
void event(json value){
 value["requestId"]=request_id;value["sequence"]=sequence++;value["bytes"]=bytes;
 value["backend"]=backend;value["permissionState"]=permission;value["deviceState"]=device_state;
 value["signalObserved"]=buffer.signal.load();frame(value);
}
struct Control {
 std::atomic<bool> stop{false};std::thread reader;
 Control():reader([this]{
  try{auto control=read_frame(3,&stop);if(control.size()!=2||control.value("kind","")!="cancel"||control.value("requestId","")!=request_id)buffer.failure.store(3);}
  catch(...){}
  if(!stop.load()){cancelled.store(true);buffer.accepting.store(false);}
 }){}
 ~Control(){stop.store(true);if(reader.joinable())reader.join();}
};
const char *error_code(ma_result result){
 if(result==MA_ACCESS_DENIED)return "PERMISSION_DENIED";
 if(result==MA_OUT_OF_MEMORY)return "AUDIO_LIMIT";
 if(result==MA_FORMAT_NOT_SUPPORTED||result==MA_INVALID_DEVICE_CONFIG)return "AUDIO_FORMAT";
 return "DEVICE_UNAVAILABLE";
}
void device_error(ma_result result){
 if(result==MA_SUCCESS)return;
 if(result==MA_ACCESS_DENIED)permission="denied";
 else permission="unknown";
 device_state="unavailable";
 throw CaptureFailure(error_code(result),"default microphone could not be opened; check device and OS microphone settings");
}
bool valid_uuid(const std::string &id){
 if(id.size()!=36)return false;
 for(size_t i=0;i<id.size();i++){if(i==8||i==13||i==18||i==23){if(id[i]!='-')return false;}else if(!std::isxdigit(static_cast<unsigned char>(id[i])))return false;}
 return true;
}
void validate_destination(){
#ifdef _WIN32
 struct _stat64 st;if(_fstat64(worker_fd(4),&st)||!(st.st_mode&_S_IFREG)||st.st_size!=0)throw CaptureFailure("INVALID_INPUT","capture requires an empty regular descriptor");
#else
 struct stat st;if(fstat(worker_fd(4),&st)||!S_ISREG(st.st_mode)||st.st_size!=0)throw CaptureFailure("INVALID_INPUT","capture requires an empty regular descriptor");
#endif
}
void drain(){
 std::array<float,1600> chunk;
 while(auto count=buffer.pop(chunk.data(),chunk.size())){
  write_all(4,chunk.data(),size_t(count)*sizeof(float));bytes+=int64_t(count)*4;
  worker_sync(4); // No progress is reported before these bytes are durable.
  event({{"kind","progress"},{"sampleRate",16000}});
 }
}
#ifndef TROVE_CAPTURE_FIXTURE_ONLY
struct Device {
 ma_context context{};ma_device device{};bool context_ready=false,device_ready=false;
 ~Device(){stop();}
 void stop(){
  buffer.accepting.store(false);
  if(device_ready){ma_device_uninit(&device);device_ready=false;}
  if(context_ready){ma_context_uninit(&context);context_ready=false;}
 }
 void start(){
  ma_context_config context_config=ma_context_config_init();
#ifdef _WIN32
  backend="wasapi";ma_backend selected=ma_backend_wasapi;
#else
  backend="pulseaudio";ma_backend selected=ma_backend_pulseaudio;
  // Initial Ubuntu support is the current user's local desktop audio server.
  // No inherited PULSE_SERVER, remote address, autospawn or ALSA plugin fallback.
  std::string runtime="/run/user/"+std::to_string(getuid());
  std::string socket=runtime+"/pulse/native";
  struct stat dir{},node{};
  if(lstat(runtime.c_str(),&dir)||!S_ISDIR(dir.st_mode)||dir.st_uid!=getuid()||(dir.st_mode&0022)||
     lstat(socket.c_str(),&node)||!S_ISSOCK(node.st_mode)||node.st_uid!=getuid()){
   permission="unknown";device_state="unavailable";
   throw CaptureFailure("DEVICE_UNAVAILABLE","local per-user PulseAudio/PipeWire socket is unavailable");
  }
  std::string server="unix:"+socket;
  context_config.pulse.pServerName=server.c_str();
  context_config.pulse.pApplicationName="Trove microphone";
  context_config.pulse.tryAutoSpawn=MA_FALSE;
#endif
  permission="unknown";
  device_error(ma_context_init(&selected,1,&context_config,&context));context_ready=true;
  ma_device_config config=ma_device_config_init(ma_device_type_capture);
  config.capture.format=ma_format_f32;config.capture.channels=1;config.sampleRate=16000;
  config.periodSizeInMilliseconds=20;config.capture.shareMode=ma_share_mode_shared;
  config.dataCallback=[](ma_device*,void*,const void *input,ma_uint32 count){buffer.push(static_cast<const float*>(input),count);};
  config.notificationCallback=[](const ma_device_notification *notification){
   if((notification->type==ma_device_notification_type_stopped||notification->type==ma_device_notification_type_interruption_began)&&buffer.accepting.load()){
    buffer.failure.store(3);buffer.accepting.store(false);
   }
  };
  device_error(ma_device_init(&context,&config,&device));device_ready=true;
  if(cancelled.load()){buffer.accepting.store(false);return;}
  device_error(ma_device_start(&device));permission="access_opened";device_state="opened";
 }
};
#endif
void record(const json &request){
 static const std::vector<std::string> keys={"protocolVersion","requestId","kind","durationMs","maxBytes","synthetic","requestPermission"};
 if(!request.is_object()||request.size()!=keys.size())throw CaptureFailure("INVALID_INPUT","unexpected capture request fields");
 for(const auto &key:keys)if(!request.contains(key))throw CaptureFailure("INVALID_INPUT","missing capture request field");
 if(!request["requestId"].is_string())throw CaptureFailure("INVALID_INPUT","invalid capture identity");
 request_id=request["requestId"].get<std::string>();
 if(!valid_uuid(request_id)||request["protocolVersion"]!=1||request["kind"]!="record"||
    !request["durationMs"].is_number_integer()||!request["maxBytes"].is_number_integer()||
    !request["synthetic"].is_boolean()||!request["requestPermission"].is_boolean())throw CaptureFailure("INVALID_INPUT","invalid capture request");
 int64_t duration=request["durationMs"],maximum=request["maxBytes"];
 if(duration<1||duration>7200000||maximum<4||maximum>512LL<<20||maximum%4)throw CaptureFailure("INVALID_INPUT","capture bounds");
 synthetic=request["synthetic"];
 if(synthetic)backend="synthetic";
#ifndef TROVE_CAPTURE_FIXTURE_ONLY
 if(synthetic)throw CaptureFailure("INVALID_INPUT","synthetic capture is absent from production builds");
#endif
 if(!synthetic&&!request["requestPermission"].get<bool>())throw CaptureFailure("APPROVAL_REQUIRED","explicit microphone action is required");
 validate_destination();buffer.maximum=std::min<uint64_t>(uint64_t(duration)*16,uint64_t(maximum)/4);
 // EOF from a crashed/exited parent stops capture and flushes queued audio.
 Control control;
#ifndef TROVE_CAPTURE_FIXTURE_ONLY
 Device device;
 if(!synthetic)device.start();
#else
 if(!synthetic)throw CaptureFailure("UNSUPPORTED_PLATFORM","this fixture binary has no device backend");
#endif
 auto started=std::chrono::steady_clock::now(),deadline=started+std::chrono::milliseconds(duration),last_data=started;
 uint64_t previous_frames=0;
 while(buffer.accepting.load()&&!cancelled.load()&&std::chrono::steady_clock::now()<deadline){
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
  {
   std::array<float,800> chunk;uint64_t at=buffer.written.load();
   for(size_t i=0;i<chunk.size();i++)chunk[i]=0.05f*std::sin(float(at+i)*2.f*3.14159265358979323846f*440.f/16000.f);
   buffer.push(chunk.data(),chunk.size());
  }
#endif
  drain();
  auto frames=buffer.written.load();
  if(frames!=previous_frames){previous_frames=frames;last_data=std::chrono::steady_clock::now();}
  else if(!synthetic&&std::chrono::steady_clock::now()-last_data>std::chrono::seconds(5)){
   device_state="interrupted";throw CaptureFailure("DEVICE_INTERRUPTED","microphone stopped delivering frames");
  }
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
  std::this_thread::sleep_for(std::chrono::milliseconds(50));
#else
  std::this_thread::sleep_for(std::chrono::milliseconds(5));
#endif
 }
 buffer.accepting.store(false);
#ifndef TROVE_CAPTURE_FIXTURE_ONLY
 device.stop();
#endif
 drain();worker_sync(4);
 switch(buffer.failure.load()){
  case 1:throw CaptureFailure("CAPTURE_OVERFLOW","capture stopped because its bounded buffer could not preserve all incoming frames");
  case 2:throw CaptureFailure("AUDIO_FORMAT","capture returned invalid PCM samples");
  case 3:device_state="interrupted";throw CaptureFailure("DEVICE_INTERRUPTED","capture device or control channel was interrupted");
 }
 if(bytes==0&&!cancelled.load())throw CaptureFailure("DEVICE_UNAVAILABLE","microphone did not provide any audio frames");
 event({{"kind","result"},{"sampleRate",16000},{"channels",1},{"format","f32le"},{"synthetic",synthetic},{"cancelled",cancelled.load()}});
}
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
int self_test(){
 CaptureBuffer test;test.maximum=CaptureBuffer::capacity+100;
 std::array<float,CaptureBuffer::capacity> input;input.fill(.25f);
 test.push(input.data(),input.size());test.push(input.data(),1);
 if(test.failure.load()!=1||test.written.load()!=input.size())return 1;
 std::array<float,CaptureBuffer::capacity> output;
 if(test.pop(output.data(),output.size())!=input.size()||output[0]!=.25f||output.back()!=.25f)return 2;
 CaptureBuffer invalid;invalid.maximum=10;float nan=std::numeric_limits<float>::quiet_NaN();invalid.push(&nan,1);
 if(invalid.failure.load()!=2||invalid.written.load()!=0)return 3;
 CaptureBuffer bounded;bounded.maximum=3;bounded.push(input.data(),10);
 if(bounded.written.load()!=3||bounded.accepting.load())return 4;
 if(std::string(error_code(MA_ACCESS_DENIED))!="PERMISSION_DENIED"||std::string(error_code(MA_NO_DEVICE))!="DEVICE_UNAVAILABLE")return 5;
 frame({{"selfTest","PASS"},{"cases",5}});return 0;
}
#endif
int main(int argc,char **argv){
 try{
  worker_init(2);
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
  if(argc==2&&std::string(argv[1])=="--self-test"){char gate;if(worker_read(0,&gate,1)!=1||gate!=1)return 1;return self_test();}
#endif
  if(argc!=1)return 1;
  frame({{"kind","hello"},{"protocolVersion",1},{"runtime","microphone"},{"runtimeCommit",CAPTURE_COMMIT},
   {"architecture",WORKER_ARCHITECTURE},{"capabilities",json::array({
#ifdef TROVE_CAPTURE_FIXTURE_ONLY
    "record_synthetic"
#else
    "record"
#endif
   })}});
  record(read_frame(0));return 0;
 }catch(const CaptureFailure &error){
  buffer.accepting.store(false);try{worker_sync(4);event({{"kind","error"},{"code",error.code},{"message",error.what()}});}catch(...){}return 1;
 }catch(const std::exception &){
  buffer.accepting.store(false);try{worker_sync(4);event({{"kind","error"},{"code","INVALID_INPUT"},{"message","capture request failed safely"}});}catch(...){}return 1;
 }
}
