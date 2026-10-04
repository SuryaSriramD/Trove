import Foundation
import AVFoundation
import Darwin

// The helper only prompts after an explicit record request with requestPermission=true.
let output = FileHandle.standardOutput
let lock = NSLock()
var stopped = false
var count: Int64 = 0
var sequence: Int64 = 0
var requestID = ""
func readBytes(_ fd: Int32, _ count: Int) throws -> Data {
    var data = Data(count: count); var offset = 0
    try data.withUnsafeMutableBytes { bytes in
        while offset < count { let n = Darwin.read(fd, bytes.baseAddress!.advanced(by: offset), min(65536, count-offset)); if n <= 0 { throw NSError(domain:"pipe",code:1) }; offset += n }
    }; return data
}
func readFrame(_ fd:Int32) throws -> [String:Any] { let h = try readBytes(fd,4); let n = h.reduce(UInt32(0)) { ($0<<8)|UInt32($1) }; if n==0 || n>1048576 { throw NSError(domain:"length",code:1) }; return try JSONSerialization.jsonObject(with:readBytes(fd,Int(n))) as! [String:Any] }
func frame(_ value:[String:Any]) { guard let b=try? JSONSerialization.data(withJSONObject:value) else { return }; var n=UInt32(b.count).bigEndian; output.write(Data(bytes:&n,count:4));output.write(b) }
func event(_ value:[String:Any]) { var v=value; v["requestId"]=requestID;v["sequence"]=sequence;sequence += 1;frame(v) }
func isStopped()->Bool {lock.lock();defer{lock.unlock()};return stopped}
func stop(){lock.lock();stopped=true;lock.unlock()}
frame(["kind":"hello","protocolVersion":1,"runtime":"microphone","runtimeCommit":"trove-microphone-v1","architecture":"arm64","capabilities":["record"]])
do {
 let req=try readFrame(0);requestID=req["requestId"] as? String ?? ""
 guard req["protocolVersion"] as? Int == 1, req["kind"] as? String == "record", let duration=req["durationMs"] as? Int, duration>0, duration<=7200000, let maxBytes=req["maxBytes"] as? Int64, maxBytes>0, maxBytes<=512*1024*1024 else {throw NSError(domain:"INVALID_INPUT",code:1)}
 let audio=FileHandle(fileDescriptor:4,closeOnDealloc:false)
 // The parent owns this pipe. EOF also means the desktop app has crashed or
 // exited, so capture must stop even when no cancellation frame can arrive.
 DispatchQueue.global().async { do { let control=try readFrame(3); if control["kind"] as? String == "cancel", control["requestId"] as? String == requestID {stop()} } catch {stop()} }
 if req["synthetic"] as? Bool == true {
  // Pace the fixture in 50 ms chunks so native cancellation, parent death and
  // flushed-audio recovery can be exercised without accessing a microphone.
  let frames=min(Int64(duration)*16,maxBytes/4)
  while count/4 < frames && !isStopped() {
   let start=count/4;let length=min(Int64(800),frames-start)
   var samples=[Float](repeating:0,count:Int(length))
   for i in 0..<samples.count {samples[i]=0.05*sin(Float(start+Int64(i))*2*Float.pi*440/16000)}
   samples.withUnsafeBytes{audio.write(Data($0))};count += length*4;try audio.synchronize()
   event(["kind":"progress","bytes":count,"sampleRate":16000])
   RunLoop.current.run(until:Date().addingTimeInterval(Double(length)/16000))
  }
  try audio.synchronize();event(["kind":"result","bytes":count,"sampleRate":16000,"channels":1,"format":"f32le","synthetic":true,"cancelled":isStopped()]);exit(0)
 }
 guard req["requestPermission"] as? Bool == true else {throw NSError(domain:"APPROVAL_REQUIRED",code:1)}
 var granted=AVCaptureDevice.authorizationStatus(for:.audio) == .authorized
 if AVCaptureDevice.authorizationStatus(for:.audio) == .notDetermined {let sem=DispatchSemaphore(value:0);AVCaptureDevice.requestAccess(for:.audio){value in granted=value;sem.signal()};while sem.wait(timeout:.now()+0.1) == .timedOut {if isStopped(){throw NSError(domain:"CANCELLED",code:1)}}}
 guard !isStopped() else {throw NSError(domain:"CANCELLED",code:1)}
 guard granted else {throw NSError(domain:"PERMISSION_DENIED",code:1)}
 let engine=AVAudioEngine();let node=engine.inputNode;let inputFormat=node.outputFormat(forBus:0)
 guard let target=AVAudioFormat(commonFormat:.pcmFormatFloat32,sampleRate:16000,channels:1,interleaved:false),let converter=AVAudioConverter(from:inputFormat,to:target) else {throw NSError(domain:"AUDIO_FORMAT",code:1)}
 node.installTap(onBus:0,bufferSize:4096,format:inputFormat){buffer,_ in
  if isStopped(){return};let capacity=AVAudioFrameCount(Double(buffer.frameLength)*16000/inputFormat.sampleRate+32)
  guard let converted=AVAudioPCMBuffer(pcmFormat:target,frameCapacity:capacity) else{return};var provided=false;var error:NSError?
  converter.convert(to:converted,error:&error){_,status in if provided {status.pointee = .noDataNow;return nil};provided=true;status.pointee = .haveData;return buffer}
  if error != nil {stop();return};if let channels=converted.floatChannelData {lock.lock();let n=min(Int64(converted.frameLength)*4,maxBytes-count);if n>0{audio.write(Data(bytes:channels[0],count:Int(n)));count += n;try? audio.synchronize()};if count>=maxBytes{stopped=true};lock.unlock()}
 }
 try engine.start();let deadline=Date().addingTimeInterval(Double(duration)/1000);var last=Date.distantPast
 while !isStopped() && Date()<deadline {RunLoop.current.run(until:Date().addingTimeInterval(0.05));if Date().timeIntervalSince(last)>1 {lock.lock();let n=count;lock.unlock();event(["kind":"progress","bytes":n,"sampleRate":16000]);last=Date()}}
 engine.stop();node.removeTap(onBus:0);try audio.synchronize();event(["kind":"result","bytes":count,"sampleRate":16000,"channels":1,"format":"f32le","synthetic":false,"cancelled":isStopped()])
} catch {event(["kind":"error","code":(error as NSError).domain,"message":"Microphone capture could not complete"]);exit(1)}
