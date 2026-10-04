# Local meeting audio decoding (CORE-005)

The macOS 27 Apple Silicon build normalizes WAV, MP3, AAC/ADTS and M4A recordings before local transcription. It accepts mono or stereo input from 8–192 kHz, with a maximum original size of 512 MiB and a maximum decoded duration of two hours. The existing 16 kHz mono PCM WAV/raw-float path also remains available.

The imported file remains the canonical recording, with its original bytes, name and file identity. Normalization produces a disposable 16 kHz mono float32 staging file. Errors and cancellation discard that derived output. The existing failed-job retry path uses the same original recording and speech pack; no new canonical schema or migration is needed.

The signed `audio-decoder` helper uses the macOS CoreAudio decoder through read-only descriptor callbacks. The helper never receives a workspace path or database handle. Input format is determined from content, independently of the filename. The supervisor verifies the helper hash, copies the authorized input to a private descriptor, enforces a three-minute timeout and 512 MiB resident-memory budget, checks the decoded output shape, and kills the helper process group on cancellation. The worker validates every float and removes temporary output after use.

`audio-decoder.json` records the helper/source hashes, platform, bounded capabilities and original-retention behavior. Available codecs are platform dependent. Other containers, unsupported codec profiles, encrypted media and damaged inputs may return recoverable errors. Playback of the original still depends on the platform's media support. No cloud decoder, ffmpeg installation or additional end-user runtime is required.

`scripts/build-native.py` and `scripts/build-desktop.py` build and include this helper. To build it independently:

```sh
python scripts/build-audio-decoder.py --output build/native
```

Focused checks include the actual compiled helper against committed, hash-recorded synthetic WAV/MP3/AAC/M4A fixtures; stereo resampling and finite non-silent output; malformed/unsupported input; input-size and destination bounds; helper tampering; prompt cancellation; derived-file cleanup; and the canonical transcription job's failure/retry/original-retention path. The fake speech engine used in the job test isolates persistence behavior. The native fixture test does not use a speech model.

With the pinned Go executable on `PATH`:

```sh
TROVE_NATIVE_INTEGRATION=1 TROVE_NATIVE_BUNDLE="$PWD/build/native" \
  go test -race -count=1 -run 'Test(AudioDecoder|RealAudioDecoder)' ./internal/native
go test -race -count=1 -tags 'fts5 sqlite_omit_load_extension' \
  -run 'Test(NormalizedAudio|RestrictedAudio|AudioRejects)' ./internal/workers
```

The fixtures contain generated sine tones, not personal recordings. `scripts/generate-audio-fixtures.py` records the generation recipe, encoder version and hashes. ffmpeg is needed only when deliberately regenerating those source fixtures.

These checks establish bounded decoding behavior on the development Mac. They do not establish speech recognition accuracy, representative one-hour meeting quality, clean-Mac installation, public distribution, or Windows/Linux support. Those release gates remain independent.
