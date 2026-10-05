// Bounded, descriptor-only audio normalization. No URL, filename or protocol
// reaches libavformat; the build contains only the declared audio components.
#include "worker-platform.hpp"
#include <nlohmann/json.hpp>
#include <chrono>
#include <cmath>
#include <memory>
#include <sys/stat.h>
extern "C" {
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/opt.h>
#include <libswresample/swresample.h>
}
using json=nlohmann::json;
constexpr int64_t AUDIO_MAX_INPUT=512LL<<20, AUDIO_MAX_FRAMES=16000LL*7200;
struct Failure:std::runtime_error {
 std::string code;
 Failure(const char *code,const char *message):std::runtime_error(message),code(code){}
};
[[noreturn]] void corrupt(const char *message){throw Failure("CORRUPT_AUDIO",message);}
[[noreturn]] void unsupported(const char *message){throw Failure("UNSUPPORTED_FORMAT",message);}
[[noreturn]] void limit(const char *message){throw Failure("AUDIO_LIMIT",message);}
void check(int result,const char *message){if(result<0)corrupt(message);}
int64_t file_size(int slot) {
#ifdef _WIN32
 struct _stat64 st;
 if(_fstat64(worker_fd(slot),&st)||!(st.st_mode&_S_IFREG))corrupt("descriptor is not a regular file");
#else
 struct stat st;
 if(fstat(worker_fd(slot),&st)||!S_ISREG(st.st_mode))corrupt("descriptor is not a regular file");
#endif
 return st.st_size;
}
uint32_t little32(const unsigned char *p){return uint32_t(p[0])|(uint32_t(p[1])<<8)|(uint32_t(p[2])<<16)|(uint32_t(p[3])<<24);}
struct Reader {
 int64_t size,offset=0;
 std::chrono::steady_clock::time_point deadline=std::chrono::steady_clock::now()+std::chrono::minutes(3);
 static int interrupted(void *opaque){return std::chrono::steady_clock::now()>=static_cast<Reader*>(opaque)->deadline;}
 static int read(void *opaque,uint8_t *buffer,int count){
  auto &r=*static_cast<Reader*>(opaque);if(interrupted(opaque))return AVERROR_EXIT;
  if(r.offset>=r.size)return AVERROR_EOF;
  count=int(std::min<int64_t>(count,r.size-r.offset));
  int n=int(worker_read_at(3,buffer,count,r.offset));
  if(n<=0)return n==0?AVERROR_EOF:AVERROR(EIO);
  r.offset+=n;return n;
 }
 static int64_t seek(void *opaque,int64_t offset,int whence){
  auto &r=*static_cast<Reader*>(opaque);
  if(whence==AVSEEK_SIZE)return r.size;
  whence &= ~AVSEEK_FORCE;
  int64_t base=whence==SEEK_SET?0:whence==SEEK_CUR?r.offset:whence==SEEK_END?r.size:-1;
  if(base<0||offset < -base||offset>r.size-base)return AVERROR(EINVAL);
  r.offset=base+offset;return r.offset;
 }
};
struct Decoder {
 AVFormatContext *format=nullptr;AVIOContext *io=nullptr;AVCodecContext *codec=nullptr;
 SwrContext *swr=nullptr;AVFrame *frame=av_frame_alloc();AVPacket *packet=av_packet_alloc();
 AVChannelLayout source_layout{};int source_format=-1,rate=0,channels=0;int64_t frames=0;
 ~Decoder(){av_packet_free(&packet);av_frame_free(&frame);swr_free(&swr);
  av_channel_layout_uninit(&source_layout);avcodec_free_context(&codec);avformat_close_input(&format);
  if(io){av_freep(&io->buffer);avio_context_free(&io);}
 }
 void write_samples(const float *data,int count){
  if(count<0||frames>AUDIO_MAX_FRAMES-count)limit("decoded duration exceeds two hours");
  for(int i=0;i<count;i++)if(!std::isfinite(data[i])||std::fabs(data[i])>1.01f)corrupt("non-finite or out-of-range audio sample");
  size_t bytes=size_t(count)*sizeof(float),at=0;
  while(at<bytes){auto n=worker_write(4,reinterpret_cast<const unsigned char*>(data)+at,bytes-at);if(n<=0)corrupt("cannot write normalized audio");at+=size_t(n);}
  frames+=count;
 }
 void convert(bool flush=false){
  if(!flush){
   if(frame->nb_samples<=0||frame->nb_samples>65536)limit("audio frame sample bound");
   if(frame->sample_rate<8000||frame->sample_rate>192000||frame->ch_layout.nb_channels<1||frame->ch_layout.nb_channels>2)unsupported("only mono/stereo 8-192 kHz audio is supported");
   if(!swr){
    rate=frame->sample_rate;channels=frame->ch_layout.nb_channels;source_format=frame->format;
    check(av_channel_layout_copy(&source_layout,&frame->ch_layout),"channel layout");
    AVChannelLayout mono=AV_CHANNEL_LAYOUT_MONO;
    check(swr_alloc_set_opts2(&swr,&mono,AV_SAMPLE_FMT_FLT,16000,&source_layout,AVSampleFormat(source_format),rate,0,nullptr),"resampler allocation");
    check(av_opt_set_double(swr,"rematrix_maxval",1.0,0),"resampler downmix");
    check(swr_init(swr),"resampler initialization");
   } else if(rate!=frame->sample_rate||source_format!=frame->format||av_channel_layout_compare(&source_layout,&frame->ch_layout))unsupported("changing audio stream configuration");
  }
  if(!swr)return;
  int64_t count=av_rescale_rnd(swr_get_delay(swr,rate)+(flush?0:frame->nb_samples),16000,rate,AV_ROUND_UP)+256;
  if(count<=0||count>262144)limit("resampler output bound");
  std::vector<float> output(size_t(count),0);
  uint8_t *planes[]={reinterpret_cast<uint8_t*>(output.data())};
  int produced=swr_convert(swr,planes,int(count),flush?nullptr:const_cast<const uint8_t**>(frame->extended_data),flush?0:frame->nb_samples);
  check(produced,"audio resampling");write_samples(output.data(),produced);
  if(flush&&produced)convert(true);
 }
 void drain(){
  while(true){int status=avcodec_receive_frame(codec,frame);
   if(status==AVERROR(EAGAIN)||status==AVERROR_EOF)return;
   check(status,"corrupt decoded audio frame");convert();av_frame_unref(frame);
  }
 }
};
bool allowed_codec(AVCodecID id){
 switch(id){case AV_CODEC_ID_AAC:case AV_CODEC_ID_MP3:case AV_CODEC_ID_PCM_U8:case AV_CODEC_ID_PCM_S16LE:
 case AV_CODEC_ID_PCM_S24LE:case AV_CODEC_ID_PCM_S32LE:case AV_CODEC_ID_PCM_F32LE:case AV_CODEC_ID_PCM_F64LE:return true;default:return false;}
}
json decode(){
 std::string request;char buf[1024];int n;
 while((n=int(worker_read(0,buf,sizeof(buf))))>0){request.append(buf,n);if(request.size()>4096)limit("request limit");}
 if(n<0)corrupt("request read failure");
 json value=json::parse(request,nullptr,false);
 if(value.is_discarded()||!value.is_object()||value.size()!=1||!value.contains("schemaVersion")||!value["schemaVersion"].is_number_integer()||value["schemaVersion"]!=1)corrupt("invalid decoder request");
 int64_t size=file_size(3);if(size<=0)corrupt("empty recording");if(size>AUDIO_MAX_INPUT)limit("input exceeds 512 MiB");
 if(file_size(4)!=0)corrupt("output must be empty");
 unsigned char header[12]={};if(worker_read_at(3,header,sizeof(header),0)<12)unsupported("unrecognized audio container");
 std::string container,demuxer;
 if(!memcmp(header,"RIFF",4)&&!memcmp(header+8,"WAVE",4)){
  container="WAV";demuxer="wav";
  if(uint64_t(little32(header+4))+8>uint64_t(size))corrupt("truncated RIFF container");
 }else if(!memcmp(header+4,"ftyp",4)){container="M4A";demuxer="mov";}
 else if(header[0]==0xff&&(header[1]&0xf6)==0xf0){container="AAC";demuxer="aac";}
 else if(!memcmp(header,"ID3",3)||(header[0]==0xff&&(header[1]&0xe0)==0xe0)){container="MP3";demuxer="mp3";}
 else unsupported("only WAV, MP3, AAC and M4A containers are supported");
 av_log_set_level(AV_LOG_QUIET);av_max_alloc(64<<20);
 Reader reader{size};Decoder d;
 if(!d.frame||!d.packet)limit("decoder allocation");
 auto *buffer=static_cast<unsigned char*>(av_malloc(32768));if(!buffer)limit("input allocation");
 d.io=avio_alloc_context(buffer,32768,0,&reader,Reader::read,nullptr,Reader::seek);
 if(!d.io){av_free(buffer);limit("input context allocation");}
 d.format=avformat_alloc_context();if(!d.format)limit("format allocation");
 d.format->pb=d.io;d.format->flags|=AVFMT_FLAG_CUSTOM_IO;
 d.format->probesize=1<<20;d.format->max_analyze_duration=5*AV_TIME_BASE;d.format->max_streams=8;
 d.format->interrupt_callback={Reader::interrupted,&reader};
 d.format->io_open=[](AVFormatContext*,AVIOContext**,const char*,int,AVDictionary**){return AVERROR(EPERM);};
 AVDictionary *options=nullptr;
 av_dict_set(&options,"protocol_whitelist","",0);
 av_dict_set(&options,"enable_drefs","0",0);av_dict_set(&options,"use_absolute_path","0",0);
 const AVInputFormat *input=av_find_input_format(demuxer.c_str());if(!input)unsupported("container is absent from restricted build");
 int opened=avformat_open_input(&d.format,nullptr,input,&options);av_dict_free(&options);check(opened,"corrupt audio container");
 check(avformat_find_stream_info(d.format,nullptr),"cannot read audio stream");
 int stream=-1;
 for(unsigned i=0;i<d.format->nb_streams;i++){
  auto *p=d.format->streams[i]->codecpar;
  if(p->codec_type==AVMEDIA_TYPE_AUDIO){if(stream!=-1)unsupported("multiple audio streams are unsupported");stream=int(i);}
  else if(p->codec_type!=AVMEDIA_TYPE_ATTACHMENT&&!(d.format->streams[i]->disposition&AV_DISPOSITION_ATTACHED_PIC))unsupported("non-audio media stream is unsupported");
 }
 if(stream<0)unsupported("no supported audio stream");
 auto *parameters=d.format->streams[stream]->codecpar;
 if(!allowed_codec(parameters->codec_id))unsupported("codec is outside the restricted decoder set");
 if(parameters->sample_rate<8000||parameters->sample_rate>192000||parameters->ch_layout.nb_channels<1||parameters->ch_layout.nb_channels>2)unsupported("only mono/stereo 8-192 kHz audio is supported");
 const AVCodec *codec=avcodec_find_decoder(parameters->codec_id);if(!codec)unsupported("codec is absent from restricted build");
 d.codec=avcodec_alloc_context3(codec);if(!d.codec)limit("codec allocation");
 check(avcodec_parameters_to_context(d.codec,parameters),"codec parameters");d.codec->thread_count=1;
 d.codec->err_recognition=AV_EF_CRCCHECK|AV_EF_BITSTREAM|AV_EF_BUFFER|AV_EF_EXPLODE;
 check(avcodec_open2(d.codec,codec,nullptr),"cannot initialize audio decoder");
 while(true){
  if(Reader::interrupted(&reader))limit("audio decoding deadline");
  int status=av_read_frame(d.format,d.packet);if(status==AVERROR_EOF)break;check(status,"corrupt audio packet");
  if(d.packet->size<0||d.packet->size>16<<20)limit("audio packet limit");
  if(d.packet->stream_index==stream){
   if(d.packet->flags&AV_PKT_FLAG_CORRUPT)corrupt("corrupt audio packet");
   check(avcodec_send_packet(d.codec,d.packet),"cannot decode audio packet");d.drain();
  }
  av_packet_unref(d.packet);
 }
 check(avcodec_send_packet(d.codec,nullptr),"cannot finish audio decode");d.drain();d.convert(true);
 if(d.frames==0)corrupt("no decoded samples");
 worker_sync(4);
 return {{"schemaVersion",1},{"frames",d.frames},{"bytes",d.frames*4},{"sampleRate",16000},{"channels",1},
  {"format","f32le"},{"container",container},{"sourceSampleRate",d.rate},{"sourceChannels",d.channels}};
}
int main(){
 try{worker_init(2);std::string result=decode().dump();if(worker_write(1,result.data(),result.size())!=int64_t(result.size()))return 1;return 0;}
 catch(const Failure &e){std::string result=json{{"schemaVersion",1},{"code",e.code},{"message",e.what()}}.dump();worker_write(1,result.data(),result.size());return 1;}
 catch(const std::exception &){std::string result=json{{"schemaVersion",1},{"code","CORRUPT_AUDIO"},{"message","decoder failed safely"}}.dump();worker_write(1,result.data(),result.size());return 1;}
}
