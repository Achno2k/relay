import Foundation
import RelayKit
import ImageIO
import UniformTypeIdentifiers

/// Turns picked files into what we upload: images become JPEGs of at most 2048 px, anything else goes as is.
enum AttachmentProcessing {
    static let maxBytes = 20 * 1024 * 1024
    static let maxCount = 10
    static let maxPixels = 2048

    struct Prepared: Sendable {
        var data: Data
        var name: String
        var contentType: String
        var kind: AttachmentKind
    }

    enum Failure: LocalizedError, Equatable {
        case tooLarge(name: String)
        case unreadableImage(name: String)

        var errorDescription: String? {
            switch self {
            case .tooLarge(let name): "\(name) is over 20 MB."
            case .unreadableImage(let name): "\(name) isn't an image Relay can read."
            }
        }
    }

    /// Reads a file picked in Files. Anything but an image (images are downscaled after) is checked
    /// against the limit by its size first, so a huge file is never loaded just to be turned down.
    /// Call off the main thread: this is a synchronous read of up to 20 MB.
    static func read(_ url: URL) throws -> (data: Data, type: UTType?) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let values = try? url.resourceValues(forKeys: [.contentTypeKey, .fileSizeKey])
        let type = values?.contentType ?? UTType(filenameExtension: url.pathExtension)
        if type?.conforms(to: .image) != true, let size = values?.fileSize, size > maxBytes {
            throw Failure.tooLarge(name: url.lastPathComponent)
        }
        return (try Data(contentsOf: url, options: .mappedIfSafe), type)
    }

    /// `name` is the original file name; `type` its UTType if known.
    static func prepare(data: Data, name: String, type: UTType?) throws -> Prepared {
        let type = type ?? UTType(filenameExtension: (name as NSString).pathExtension)
        if let type, type.conforms(to: .image) {
            return try prepareImage(data: data, name: name)
        }
        guard data.count <= maxBytes else { throw Failure.tooLarge(name: name) }
        let mime = type?.preferredMIMEType ?? "application/octet-stream"
        let kind: AttachmentKind = type?.conforms(to: .pdf) == true ? .pdf : .file
        return Prepared(data: data, name: name, contentType: mime, kind: kind)
    }

    /// Downscales (never upscales) and re-encodes as JPEG, so HEIC and PNG screenshots arrive as small JPEGs.
    static func prepareImage(data: Data, name: String) throws -> Prepared {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { throw Failure.unreadableImage(name: name) }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixels,
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else {
            throw Failure.unreadableImage(name: name)
        }
        let out = NSMutableData()
        guard let dest = CGImageDestinationCreateWithData(out, UTType.jpeg.identifier as CFString, 1, nil) else {
            throw Failure.unreadableImage(name: name)
        }
        CGImageDestinationAddImage(dest, image, [kCGImageDestinationLossyCompressionQuality: 0.85] as CFDictionary)
        guard CGImageDestinationFinalize(dest) else { throw Failure.unreadableImage(name: name) }
        guard out.length <= maxBytes else { throw Failure.tooLarge(name: name) }
        let stem = ((name as NSString).deletingPathExtension as String)
        return Prepared(data: out as Data, name: (stem.isEmpty ? "image" : stem) + ".jpg", contentType: "image/jpeg", kind: .image)
    }
}
