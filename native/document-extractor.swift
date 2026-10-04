import Foundation
import PDFKit
import Darwin

// A one-shot, local PDF text extractor. Input/output are pipes; this helper has
// no workspace/database path, network operation, or JavaScript execution path.
let maximumInput = 128 * 1024 * 1024
let maximumOutput = 16 * 1024 * 1024
func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}
guard CommandLine.arguments.count == 1 else { fail("No path arguments are accepted") }
var input = Data()
while true {
    let bytes = FileHandle.standardInput.readData(ofLength: 65536)
    if bytes.isEmpty { break }
    guard input.count + bytes.count <= maximumInput else { fail("PDF exceeds 128 MiB extraction limit") }
    input.append(bytes)
}
guard input.prefix(5) == Data("%PDF-".utf8), let document = PDFDocument(data: input) else {
    fail("Malformed or unsupported PDF; original file retained")
}
guard !document.isLocked else { fail("Password-protected PDF; original file retained") }
guard document.pageCount > 0 && document.pageCount <= 10000 else { fail("PDF page limit exceeded") }
var written = 0
var readable = false
for index in 0..<document.pageCount {
    let text: String = autoreleasepool { document.page(at: index)?.string ?? "" }
    let bytes = Data((text + "\n\u{000C}\n").utf8)
    guard written + bytes.count <= maximumOutput else { fail("PDF text exceeds extraction limit; original retained") }
    if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { readable = true }
    FileHandle.standardOutput.write(bytes)
    written += bytes.count
}
guard readable else { fail("No extractable text; scanned PDF requires OCR; original retained") }
