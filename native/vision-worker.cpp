#include "worker.hpp"
#include "worker-projector.hpp"
#include "vision-prompt.hpp"
#include "llama.h"
#include "mtmd.h"
#include "mtmd-helper.h"
#include <memory>
static void logger(enum ggml_log_level,const char *text,void*){fputs(text,stderr);}
int main(){try{worker_init();
 llama_log_set(logger,nullptr);mtmd_helper_log_set(logger,nullptr);
 frame({{"kind","hello"},{"protocolVersion",1},{"runtime","llama.cpp"},{"runtimeCommit","d2e54583c7452353eb35d40431281f6ee984332f"},{"architecture",WORKER_ARCHITECTURE},{"capabilities",{"image_generate"}}});
 request=read_frame(0);init_control();if(request.at("operation")!="image_generate"||request.at("inputs").size()!=4)throw std::runtime_error("image operation or descriptors");
 auto input=json::parse(read_input(1,16384));std::string question=input.at("question");int context=input.at("contextTokens"),max_tokens=request.at("limits").at("maxTokens"),threads=request.at("limits").at("threads");
 if(input.at("schemaVersion")!=1||context<512||context>8192||max_tokens<1||max_tokens>512||question.empty()||question.size()>8192||input.at("width").get<int>()>1024||input.at("height").get<int>()>1024)throw std::runtime_error("image metadata bounds");
 worker_disable_gpu();llama_backend_init();auto mp=llama_model_default_params();mp.n_gpu_layers=0;mp.load_mode=LLAMA_LOAD_MODE_NONE;mp.progress_callback=[](float,void*){return !cancelled.load();};
 FILE *mf=worker_model_file();if(!mf)throw std::runtime_error("model descriptor");std::unique_ptr<llama_model,decltype(&llama_model_free)> model(llama_model_load_from_file_ptr(mf,mp),llama_model_free);fclose(mf);if(!model)throw std::runtime_error("model load failed");check_cancel();
 auto vp=mtmd_context_params_default();vp.use_gpu=false;vp.n_threads=threads;vp.warmup=false;vp.progress_callback=[](float,void*){return !cancelled.load();};
 worker_projector projector;
 std::unique_ptr<mtmd_context,decltype(&mtmd_free)> vision(mtmd_init_from_file(projector.path(),model.get(),vp),mtmd_free);if(!vision||!mtmd_support_vision(vision.get()))throw std::runtime_error("vision projector load failed");check_cancel();progress(1,"Verified model and projector loaded");
 auto image=read_input(2,5<<20);auto decoded=mtmd_helper_bitmap_init_from_buf(vision.get(),reinterpret_cast<const unsigned char*>(image.data()),image.size(),false,mtmd_helper_init_opt_default());
 std::unique_ptr<mtmd_bitmap,decltype(&mtmd_bitmap_free)> bitmap(decoded.bitmap,mtmd_bitmap_free);if(!bitmap||decoded.video_ctx)throw std::runtime_error("normalized image decode failed");
 if(mtmd_bitmap_get_nx(bitmap.get())!=input.at("width")||mtmd_bitmap_get_ny(bitmap.get())!=input.at("height"))throw std::runtime_error("image size mismatch");
 std::string prompt=vision_prompt(question,mtmd_default_marker());
 mtmd_input_text text{prompt.c_str(),prompt.size(),true,true};const mtmd_bitmap *images[]={bitmap.get()};std::unique_ptr<mtmd_input_chunks,decltype(&mtmd_input_chunks_free)> chunks(mtmd_input_chunks_init(),mtmd_input_chunks_free);
 if(mtmd_tokenize(vision.get(),chunks.get(),&text,images,1))throw std::runtime_error("multimodal tokenization failed");auto input_tokens=mtmd_helper_get_n_tokens(chunks.get());auto positions=mtmd_helper_get_n_pos(chunks.get());
 if(input_tokens<1||input_tokens+max_tokens>size_t(context)||positions<1||positions+max_tokens>context)throw std::runtime_error("RESOURCE_EXHAUSTED: image and question exceed exact context budget");check_cancel();
 auto cp=llama_context_default_params();cp.n_ctx=context;cp.n_batch=512;cp.n_ubatch=512;cp.n_threads=cp.n_threads_batch=threads;cp.abort_callback=abort_work;
 std::unique_ptr<llama_context,decltype(&llama_free)> ctx(llama_init_from_model(model.get(),cp),llama_free);if(!ctx)throw std::runtime_error("image context load failed");llama_pos past=0;
 if(mtmd_helper_eval_chunks(vision.get(),ctx.get(),chunks.get(),0,0,512,true,&past))throw std::runtime_error("image evaluation failed");check_cancel();progress(2,"Image encoded locally");
 auto vocab=llama_model_get_vocab(model.get());std::unique_ptr<llama_sampler,decltype(&llama_sampler_free)> sampler(llama_sampler_init_greedy(),llama_sampler_free);std::string output,pending,reason="length";int count=0;
 for(;count<max_tokens;count++){check_cancel();auto token=llama_sampler_sample(sampler.get(),ctx.get(),-1);if(llama_vocab_is_eog(vocab,token)){reason="eos";break;}char piece[16384];int n=llama_token_to_piece(vocab,token,piece,sizeof(piece),0,true);if(n<0||output.size()+n>16384)throw std::runtime_error("image text output limit");output.append(piece,n);pending.append(piece,n);try{json(pending).dump();event({{"kind","token"},{"text",pending}});pending.clear();}catch(const json::type_error&){}if(llama_decode(ctx.get(),llama_batch_get_one(&token,1)))throw std::runtime_error("image answer evaluation failed");}
 if(!pending.empty())throw std::runtime_error("incomplete UTF-8 output");finish({{"kind","image_generation"},{"text",output},{"finishReason",reason},{"inputTokens",input_tokens},{"outputTokens",count}});return 0;
 }catch(const std::exception&e){return failure(e);}}
