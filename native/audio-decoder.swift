// CoreAudio decodes only an authorized read-only descriptor (fd 3). The source
// filename, workspace path and database never cross this process boundary.
import AudioToolbox
import Foundation
import Darwin

let maximumFrames: Int64 = 16_000 * 7_200
let maximumInputBytes: Int64 = 512 << 20

struct DecodeFailure: Error {
    let code: String
    let message: String
}
func fail(_ code: String, _ message: String) throws -> Never {
    throw DecodeFailure(code: code, message: message)
}
func emit(_ body: [String: Any]) {
    if let data = try? JSONSerialization.data(withJSONObject: body, options: [.sortedKeys]) {
        FileHandle.standardOutput.write(data)
    }
}
final class Source {
    let size: Int64
    init(_ size: Int64) { self.size = size }
}
let readSource: AudioFile_ReadProc = { opaque, position, requested, destination, actual in
    let source = Unmanaged<Source>.fromOpaque(opaque).takeUnretainedValue()
    actual.pointee = 0
    guard position >= 0 && position <= source.size else { return kAudioFilePositionError }
    let count = Int(min(Int64(requested), source.size - position))
    let got = pread(3, destination, count, off_t(position))
    guard got >= 0 else { return kAudioFileUnspecifiedError }
    actual.pointee = UInt32(got)
    return noErr
}
let sourceSize: AudioFile_GetSizeProc = { opaque in
    Unmanaged<Source>.fromOpaque(opaque).takeUnretainedValue().size
}

func decode() throws -> [String: Any] {
    let request = FileHandle.standardInput.readDataToEndOfFile()
    guard request.count <= 4_096,
          let body = try JSONSerialization.jsonObject(with: request) as? [String: Any],
          body["schemaVersion"] as? Int == 1 else {
        try fail("CORRUPT_AUDIO", "Invalid decoder request")
    }
    var inputStat = stat()
    var outputStat = stat()
    guard fstat(3, &inputStat) == 0, (inputStat.st_mode & S_IFMT) == S_IFREG,
          inputStat.st_size > 0, inputStat.st_size <= maximumInputBytes,
          fstat(4, &outputStat) == 0, (outputStat.st_mode & S_IFMT) == S_IFREG,
          outputStat.st_size == 0 else {
        try fail("AUDIO_LIMIT", "Expected bounded input and an empty staging output")
    }
    let source = Source(inputStat.st_size)
    defer { withExtendedLifetime(source) {} }
    var header = [UInt8](repeating: 0, count: 12)
    let headerBytes = header.withUnsafeMutableBytes { pread(3, $0.baseAddress!, $0.count, 0) }
    if headerBytes == 12 && Array(header[0..<4]) == Array("RIFF".utf8) && Array(header[8..<12]) == Array("WAVE".utf8) {
        let declared = UInt32(header[4]) | UInt32(header[5]) << 8 | UInt32(header[6]) << 16 | UInt32(header[7]) << 24
        guard Int64(declared) + 8 <= inputStat.st_size else { try fail("CORRUPT_AUDIO", "Truncated WAV container") }
    }
    let opaque = Unmanaged.passUnretained(source).toOpaque()
    var audio: AudioFileID?
    let opened = AudioFileOpenWithCallbacks(opaque, readSource, nil, sourceSize, nil, 0, &audio)
    guard opened == noErr, let audio else {
        let code = opened == kAudioFileUnsupportedFileTypeError || opened == kAudioFileUnsupportedDataFormatError
            ? "UNSUPPORTED_FORMAT" : "CORRUPT_AUDIO"
        try fail(code, "CoreAudio cannot open this recording (\(opened))")
    }
    defer { AudioFileClose(audio) }
    var container: AudioFileTypeID = 0
    var bytes = UInt32(MemoryLayout<AudioFileTypeID>.size)
    guard AudioFileGetProperty(audio, kAudioFilePropertyFileFormat, &bytes, &container) == noErr else {
        try fail("CORRUPT_AUDIO", "Cannot read audio container")
    }
    let containers: [AudioFileTypeID: String] = [kAudioFileWAVEType: "WAV", kAudioFileMP3Type: "MP3",
        kAudioFileAAC_ADTSType: "AAC", kAudioFileM4AType: "M4A", kAudioFileMPEG4Type: "M4A"]
    guard let containerName = containers[container] else {
        try fail("UNSUPPORTED_FORMAT", "Supported containers are WAV, MP3, AAC and M4A")
    }
    var extended: ExtAudioFileRef?
    guard ExtAudioFileWrapAudioFileID(audio, false, &extended) == noErr, let extended else {
        try fail("CORRUPT_AUDIO", "Cannot initialize audio decoder")
    }
    defer { ExtAudioFileDispose(extended) }
    var sourceFormat = AudioStreamBasicDescription()
    bytes = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
    guard ExtAudioFileGetProperty(extended, kExtAudioFileProperty_FileDataFormat, &bytes, &sourceFormat) == noErr,
          sourceFormat.mSampleRate.isFinite, sourceFormat.mSampleRate >= 8_000,
          sourceFormat.mSampleRate <= 192_000,
          sourceFormat.mChannelsPerFrame >= 1, sourceFormat.mChannelsPerFrame <= 2 else {
        try fail("UNSUPPORTED_FORMAT", "Supported input is mono/stereo audio from 8–192 kHz")
    }
    var sourceFrames: Int64 = 0
    bytes = UInt32(MemoryLayout<Int64>.size)
    if ExtAudioFileGetProperty(extended, kExtAudioFileProperty_FileLengthFrames, &bytes, &sourceFrames) == noErr,
       Double(sourceFrames) / sourceFormat.mSampleRate > 7_200 {
        try fail("AUDIO_LIMIT", "Recording exceeds two hours")
    }
    var outputFormat = AudioStreamBasicDescription(mSampleRate: 16_000, mFormatID: kAudioFormatLinearPCM,
        mFormatFlags: kAudioFormatFlagsNativeFloatPacked, mBytesPerPacket: 4, mFramesPerPacket: 1,
        mBytesPerFrame: 4, mChannelsPerFrame: 1, mBitsPerChannel: 32, mReserved: 0)
    guard ExtAudioFileSetProperty(extended, kExtAudioFileProperty_ClientDataFormat,
        UInt32(MemoryLayout<AudioStreamBasicDescription>.size), &outputFormat) == noErr else {
        try fail("UNSUPPORTED_FORMAT", "This codec cannot be normalized by this macOS decoder")
    }
    var total: Int64 = 0
    var samples = [Float](repeating: 0, count: 4_096)
    while true {
        let count: UInt32 = try samples.withUnsafeMutableBytes { storage in
            var frames: UInt32 = 4_096
            var buffers = AudioBufferList(mNumberBuffers: 1, mBuffers: AudioBuffer(mNumberChannels: 1,
                mDataByteSize: UInt32(storage.count), mData: storage.baseAddress))
            let status = ExtAudioFileRead(extended, &frames, &buffers)
            guard status == noErr else { try fail("CORRUPT_AUDIO", "Audio decode failed (\(status))") }
            guard total + Int64(frames) <= maximumFrames else { try fail("AUDIO_LIMIT", "Recording exceeds two hours") }
            let values = storage.bindMemory(to: Float.self)
            for i in 0..<Int(frames) {
                guard values[i].isFinite && abs(values[i]) <= 1.01 else {
                    try fail("CORRUPT_AUDIO", "Audio contains invalid samples")
                }
            }
            var written = 0
            let length = Int(frames) * 4
            while written < length {
                let result = Darwin.write(4, storage.baseAddress!.advanced(by: written), length - written)
                guard result > 0 else { try fail("CORRUPT_AUDIO", "Cannot write normalized staging audio") }
                written += result
            }
            return frames
        }
        if count == 0 { break }
        total += Int64(count)
    }
    guard total > 0 else { try fail("CORRUPT_AUDIO", "Recording contains no decodable audio") }
    guard fsync(4) == 0 else { try fail("CORRUPT_AUDIO", "Cannot flush normalized audio") }
    return ["schemaVersion": 1, "frames": total, "bytes": total * 4, "sampleRate": 16_000,
            "channels": 1, "format": "f32le", "container": containerName,
            "sourceSampleRate": sourceFormat.mSampleRate, "sourceChannels": sourceFormat.mChannelsPerFrame]
}

do {
    emit(try decode())
} catch let failure as DecodeFailure {
    emit(["schemaVersion": 1, "code": failure.code, "message": failure.message])
    exit(1)
} catch {
    emit(["schemaVersion": 1, "code": "CORRUPT_AUDIO", "message": "Invalid audio data"])
    exit(1)
}
