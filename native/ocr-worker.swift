import Foundation
import Vision
import ImageIO
import Darwin

// Descriptor 3 is the sole input. No path, URL, network, model download, or
// workspace mutation protocol is accepted by this worker.
struct OCRLine: Codable { let text: String; let confidence: Float }
struct OCRResult: Codable { let lines: [OCRLine]; let warning: String }
do {
    let input = FileHandle(fileDescriptor: 3, closeOnDealloc: false)
    let bytes = input.readData(ofLength: (20 << 20) + 1)
    guard bytes.count <= 20 << 20,
          let source = CGImageSourceCreateWithData(bytes as CFData, nil),
          let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
          let width = properties[kCGImagePropertyPixelWidth] as? Int,
          let height = properties[kCGImagePropertyPixelHeight] as? Int,
          width > 0, height > 0, width <= 20000000 / height,
          let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
        throw NSError(domain: "TroveOCR", code: 1, userInfo: [NSLocalizedDescriptionKey: "Invalid or oversized image"])
    }
    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = true
    request.automaticallyDetectsLanguage = true
    try VNImageRequestHandler(cgImage: image, options: [:]).perform([request])
    let observations = (request.results ?? []).sorted { a, b in
        if abs(a.boundingBox.midY - b.boundingBox.midY) > 0.012 { return a.boundingBox.midY > b.boundingBox.midY }
        return a.boundingBox.minX < b.boundingBox.minX
    }
    guard observations.count <= 10000 else { throw NSError(domain: "TroveOCR", code: 2) }
    let lines = observations.compactMap { observation -> OCRLine? in
        guard let candidate = observation.topCandidates(1).first else { return nil }
        return OCRLine(text: candidate.string, confidence: candidate.confidence)
    }
    let result = try JSONEncoder().encode(OCRResult(lines: lines, warning: "OCR may misread handwriting, columns, low contrast, or rotated text. Verify against the image."))
    guard result.count <= 2 << 20 else { throw NSError(domain: "TroveOCR", code: 3) }
    FileHandle.standardOutput.write(result)
} catch {
    FileHandle.standardError.write(Data("Local OCR failed: \(error.localizedDescription)\n".utf8))
    exit(1)
}
