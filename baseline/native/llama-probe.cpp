#include "llama.h"
#include <cstdio>

int main() {
    llama_backend_init();
    std::printf("llama backend initialized; gpu_offload=%d\n", llama_supports_gpu_offload());
    std::printf("%s\n", llama_print_system_info());
    llama_backend_free();
    return 0;
}
