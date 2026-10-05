"""Native worker target contract. Plans do not imply execution qualification."""
import platform

TARGETS = {
    'darwin-arm64': {'os': 'darwin', 'arch': 'arm64', 'host': 'Darwin', 'minimumOS': 'macOS 27.0',
        'requirements': ['Xcode command-line tools / macOS 27 SDK', 'Swift 5', 'CMake 4.3.2', 'Apple clang 21.0.0'],
        'capture': True, 'audioDecoding': True, 'ocr': 'separate optional macOS helper'},
    'windows-amd64': {'os': 'windows', 'arch': 'amd64', 'host': 'Windows', 'minimumOS': 'Windows 11 x64',
        'requirements': ['x64 Native Tools command prompt for Visual Studio 2022 or later (MSVC v143 or newer)',
            'Windows 11 SDK', 'Python 3.12+', 'CMake 4.3.2', 'Ninja', 'NTFS local staging/cache'],
        'capture': False, 'audioDecoding': False, 'ocr': False},
    'linux-amd64': {'os': 'linux', 'arch': 'amd64', 'host': 'Linux', 'minimumOS': 'Ubuntu 24.04 LTS x64',
        'requirements': ['Ubuntu 24.04 build-essential (GCC 13)', 'libssl-dev / libssl3t64', 'Python 3.12',
            'CMake 4.3.2', 'Ninja', 'procps and mounted /proc'],
        'capture': False, 'audioDecoding': False, 'ocr': False},
}

def host_target(system=None, machine=None):
    system = platform.system() if system is None else system
    machine = platform.machine() if machine is None else machine
    arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}.get(machine.lower())
    os_name = {'Darwin': 'darwin', 'Windows': 'windows', 'Linux': 'linux'}.get(system)
    name = str(os_name) + '-' + str(arch)
    if name not in TARGETS:
        raise ValueError('Unsupported native worker host: ' + system + '/' + machine)
    return name

def require_native_host(target, system=None, machine=None):
    if target != host_target(system, machine):
        raise ValueError('Native worker builds must run on the selected OS/architecture; cross-compilation is not qualification: ' + target)

def cpu_options():
    # Portable x64 CPU baseline. No host ISA auto-detection or GPU requirement.
    return ['-DGGML_NATIVE=OFF', '-DGGML_SSE42=OFF', '-DGGML_AVX=OFF', '-DGGML_AVX2=OFF',
            '-DGGML_BMI2=OFF', '-DGGML_BLAS=OFF', '-DGGML_FMA=OFF', '-DGGML_F16C=OFF', '-DGGML_AVX512=OFF', '-DGGML_OPENMP=OFF',
            '-DGGML_METAL=OFF', '-DGGML_CUDA=OFF', '-DGGML_VULKAN=OFF', '-DBUILD_SHARED_LIBS=OFF']
