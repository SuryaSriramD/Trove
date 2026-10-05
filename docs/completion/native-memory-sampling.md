# Native worker memory sampling

The old macOS sampler launched `/bin/ps`. That installed executable is setuid and cannot run inside the offline sandbox: the reproduced result was exit 71, `Operation not permitted`. A zero value in earlier optional collectors therefore means **measurement unavailable**, never zero memory consumption. Those raw logs are retained.

macOS now reads `PROC_PIDTASKINFO.pti_resident_size` through the SDK's `libproc` API. Linux reads a bounded `/proc/<owned worker pid>/statm` record and converts resident pages using the OS page size. Neither sampler launches a command. Windows keeps its existing process-memory API and Job Object. Invalid or missing process data returns an error. A non-CGO Darwin build reports sampling unavailable; production desktop builds already require CGO.

The direct macOS check passes inside the same network-denied policy that rejected `ps`. Its independent fixture touches 8MiB of owned pages and requires at least 8MiB of resident memory. The ordinary native race suite passes; Linux compilation passes without a target-runtime claim. Exact source/SDK hashes and results are in the [manifest](../../evidence/completion/native-rss/manifest.json).

The supervisor samples at 250ms intervals. Measurements are sampled worker resident bytes, not an exact peak, total application footprint, tokenizer peak, or complete GPU/driver memory accounting. Model admission and output/time limits remain separate. Representative concurrent-load and platform memory qualification remain open.
