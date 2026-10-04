import AppKit
import WebKit
import PDFKit
import Foundation
import Darwin

// One-shot helper: only a private staged document, no workspace or app bridge.
let maximumOutput = 256 * 1024 * 1024
func fail(_ message:String)->Never{FileHandle.standardError.write(Data((message+"\n").utf8));exit(1)}
guard CommandLine.arguments.count == 2 else { fail("Expected a staged print document") }
let root=URL(fileURLWithPath:CommandLine.arguments[1],isDirectory:true).standardizedFileURL
let output=root.appendingPathComponent("result.pdf")
let report=root.appendingPathComponent("result.json")
guard root.isFileURL, root.path==root.resolvingSymlinksInPath().path else {fail("Invalid print staging directory")}
let allowedOrigin="trove-print://document"
class LocalFiles:NSObject,WKURLSchemeHandler{
 func webView(_ webView:WKWebView,start task:WKURLSchemeTask){
  guard let url=task.request.url,url.scheme=="trove-print",url.host=="document",task.request.httpMethod=="GET",url.query==nil,url.fragment==nil else {task.didFailWithError(NSError(domain:"TrovePrint",code:1));return}
  let path=url.path;let parts=path.split(separator:"/",omittingEmptySubsequences:true)
  guard !parts.contains(".."),!parts.contains("."),!path.contains("\\"),!path.contains("\0"),!parts.isEmpty else {task.didFailWithError(NSError(domain:"TrovePrint",code:2));return}
  let file=root.appendingPathComponent(parts.joined(separator:"/")).standardizedFileURL
  guard file.path.hasPrefix(root.path+"/"),file.path==file.resolvingSymlinksInPath().path, !["result.pdf","result.json"].contains(parts.first!) else {task.didFailWithError(NSError(domain:"TrovePrint",code:3));return}
  do{let attrs=try FileManager.default.attributesOfItem(atPath:file.path);guard attrs[.type] as? FileAttributeType == .typeRegular,(attrs[.size] as? NSNumber)?.intValue ?? Int.max <= 64*1024*1024 else{throw NSError(domain:"TrovePrint",code:4)}
   let data=try Data(contentsOf:file),ext=file.pathExtension.lowercased()
   var mime=["html":"text/html","js":"text/javascript","css":"text/css","json":"application/json","woff2":"font/woff2","woff":"font/woff","ttf":"font/ttf","svg":"image/svg+xml","png":"image/png","jpg":"image/jpeg","jpeg":"image/jpeg","gif":"image/gif","webp":"image/webp"][ext] ?? "application/octet-stream"
   if parts.first=="managed"{if data.starts(with:[137,80,78,71]){mime="image/png"}else if data.starts(with:[255,216,255]){mime="image/jpeg"}else if data.starts(with:Array("GIF8".utf8)){mime="image/gif"}else if String(data: data.prefix(1024),encoding:.utf8)?.contains("<svg") == true{mime="image/svg+xml"}else if data.count>12 && String(data:data[8..<12],encoding:.utf8)=="WEBP"{mime="image/webp"}}
   task.didReceive(HTTPURLResponse(url:url,statusCode:200,httpVersion:"HTTP/1.1",headerFields:["Content-Type":mime,"Content-Length":String(data.count),"Cache-Control":"no-store"])!);task.didReceive(data);task.didFinish()
  }catch{task.didFailWithError(error)}
 }
 func webView(_ webView:WKWebView,stop task:WKURLSchemeTask){}
}
let app=NSApplication.shared
app.setActivationPolicy(.prohibited)
class Printer:NSObject,WKNavigationDelegate{
 let files=LocalFiles();var view:WKWebView!;var window:NSWindow!;var printing=false;var status:[String:Any]=[:]
 func start(){
  let configuration=WKWebViewConfiguration();configuration.websiteDataStore = .nonPersistent();configuration.setURLSchemeHandler(files,forURLScheme:"trove-print");configuration.preferences.javaScriptCanOpenWindowsAutomatically=false
  // CSP controls every document resource; this independent rule also denies all network schemes.
  let rules=#"[{"trigger":{"url-filter":"^https?://"},"action":{"type":"block"}},{"trigger":{"url-filter":"^wss?://"},"action":{"type":"block"}},{"trigger":{"url-filter":"^file://"},"action":{"type":"block"}}]"#
  WKContentRuleListStore.default().compileContentRuleList(forIdentifier:"TroveOfflinePrintV1",encodedContentRuleList:rules){list,error in
   guard let list=list,error==nil else{fail("Offline request policy unavailable")};configuration.userContentController.add(list)
   self.view=WKWebView(frame:NSRect(x:0,y:0,width:794,height:1123),configuration:configuration);self.view.navigationDelegate=self
   self.window=NSWindow(contentRect:self.view.frame,styleMask:.borderless,backing:.buffered,defer:false);self.window.contentView=self.view
   self.view.load(URLRequest(url:URL(string:allowedOrigin+"/print.html")!))
  }
 }
 func webView(_ webView:WKWebView,decidePolicyFor action:WKNavigationAction,decisionHandler:@escaping(WKNavigationActionPolicy)->Void){
  decisionHandler(action.request.url?.absoluteString==allowedOrigin+"/print.html" && action.targetFrame?.isMainFrame==true ? .allow:.cancel)
 }
 func webView(_ webView:WKWebView,didFail navigation:WKNavigation!,withError error:Error){fail("Print document navigation failed")}
 func webView(_ webView:WKWebView,didFailProvisionalNavigation navigation:WKNavigation!,withError error:Error){fail("Print document could not load")}
 func webViewWebContentProcessDidTerminate(_ webView:WKWebView){fail("Print renderer terminated")}
 func webView(_ webView:WKWebView,didFinish navigation:WKNavigation!){poll()}
 func poll(){view.evaluateJavaScript("window.trovePrintStatus ?? null"){value,error in
  if let state=value as? [String:Any]{if let failure=state["error"] as? String{fail("Print preparation failed: "+String(failure.prefix(1024)))};if state["ready"] as? Bool==true{self.status=state;self.printDocument();return}}
  DispatchQueue.main.asyncAfter(deadline:.now()+0.1){self.poll()}
 }}
 func printDocument(){guard !printing else{return};printing=true
  let info=NSPrintInfo();info.paperSize=NSSize(width:595.28,height:841.89);info.leftMargin=36.85;info.rightMargin=36.85;info.topMargin=39.69;info.bottomMargin=42.52;info.horizontalPagination = .fit;info.verticalPagination = .automatic;info.isHorizontallyCentered=false;info.isVerticallyCentered=false;info.jobDisposition = .save;info.dictionary()[NSPrintInfo.AttributeKey.jobSavingURL]=output
  let operation=view.printOperation(with:info);operation.showsPrintPanel=false;operation.showsProgressPanel=false;operation.view?.frame=view.bounds
  // run() does not let WebKit's asynchronous page computation finish reliably.
  operation.runModal(for:window,delegate:self,didRun:#selector(finished(_:success:contextInfo:)),contextInfo:nil)
 }
 @objc func finished(_ operation:NSPrintOperation,success:Bool,contextInfo:UnsafeMutableRawPointer?){
  guard success,let size=(try? FileManager.default.attributesOfItem(atPath:output.path)[.size] as? NSNumber)?.intValue,size>5,size<=maximumOutput,let pdf=PDFDocument(url:output),pdf.pageCount>0,pdf.pageCount<=2000 else {fail("Native PDF conversion failed or exceeded its bounds")}
  status["physicalPages"]=pdf.pageCount
  do{try JSONSerialization.data(withJSONObject:status).write(to:report,options:.atomic);let input=try FileHandle(forReadingFrom:output);defer{try? input.close()};while true{let data=input.readData(ofLength:65536);if data.isEmpty{break};FileHandle.standardOutput.write(data)}}catch{fail("Could not return completed PDF")}
  exit(0)
 }
}
DispatchQueue.global().asyncAfter(deadline:.now()+110){fail("PDF renderer deadline exceeded")}
let watchdog=DispatchSource.makeTimerSource(queue:.global());watchdog.schedule(deadline:.now(),repeating:.milliseconds(200));watchdog.setEventHandler{if let attrs=try? FileManager.default.attributesOfItem(atPath:output.path),let size=attrs[.size] as? NSNumber,size.intValue>maximumOutput{fail("PDF output exceeds 256 MiB")}};watchdog.resume()
let printer=Printer();printer.start();app.run()
