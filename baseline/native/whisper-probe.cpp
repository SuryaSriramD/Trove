#include "whisper.h"
#include <cstdio>

int main() {
    std::printf("whisper runtime linked; version=%s\n", whisper_version());
    std::printf("%s\n", whisper_print_system_info());
    return 0;
}
