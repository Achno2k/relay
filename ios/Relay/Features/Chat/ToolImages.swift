import RelayKit
import ImageIO
import SwiftUI
import UIKit

/// Tool-result image bytes in memory, keyed by the endpoint path (`agentId/tool-images/toolCallId/index`).
/// NSCache gives them back under memory pressure; the bridge can always serve them again.
final class ToolImageCache {
    private let cache: NSCache<NSString, NSData> = {
        let cache = NSCache<NSString, NSData>()
        cache.totalCostLimit = 64 << 20
        return cache
    }()

    static func key(agentId: String, toolCallId: String, index: Int) -> String {
        "\(agentId)/tool-images/\(toolCallId)/\(index)"
    }

    subscript(key: String) -> Data? {
        get { cache.object(forKey: key as NSString) as Data? }
        set {
            if let newValue {
                cache.setObject(newValue as NSData, forKey: key as NSString, cost: newValue.count)
            } else {
                cache.removeObject(forKey: key as NSString)
            }
        }
    }
}

/// The images a tool returned, under its step row: one full-width thumbnail (at most `maxHeight` tall), or a
/// horizontal row for several. Tap one for the full-screen viewer.
struct ToolImagesView: View {
    let toolCallId: String
    let images: [ToolImage]
    let title: String
    let agentId: String
    let store: AppStore
    /// The bridge has no tool-images endpoint (404): the row goes back to its `[image]` preview.
    let onUnavailable: () -> Void

    @State private var viewing: Viewed?
    @Namespace private var zoom

    static let maxHeight: CGFloat = 260
    static let rowHeight: CGFloat = 200

    private struct Viewed: Identifiable {
        let index: Int
        let image: UIImage
        var id: Int { index }
    }

    var body: some View {
        Group {
            if images.count == 1, let image = images.first {
                tile(image).frame(maxWidth: .infinity, alignment: .leading)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(images, id: \.index) { tile($0) }
                    }
                }
                .scrollClipDisabled()
            }
        }
        .fullScreenCover(item: $viewing) { item in
            ImageViewer(image: item.image, name: title)
                .navigationTransition(.zoom(sourceID: item.index, in: zoom))
        }
    }

    private func tile(_ image: ToolImage) -> some View {
        ToolImageTile(
            image: image,
            maxHeight: images.count == 1 ? Self.maxHeight : Self.rowHeight,
            fixedHeight: images.count > 1,
            label: images.count == 1 ? title : "\(title), image \(image.index + 1) of \(images.count)",
            load: { try await store.toolImageData(agentId: agentId, toolCallId: toolCallId, index: image.index) },
            onOpen: { viewing = Viewed(index: image.index, image: $0) },
            onUnavailable: onUnavailable
        )
        .matchedTransitionSource(id: image.index, in: zoom)
    }
}

private struct ToolImageTile: View {
    let image: ToolImage
    let maxHeight: CGFloat
    /// In a row every tile is `maxHeight` tall; alone, it's the full width up to `maxHeight`.
    let fixedHeight: Bool
    let label: String
    let load: () async throws -> Data
    let onOpen: (UIImage) -> Void
    let onUnavailable: () -> Void

    @State private var thumbnail: UIImage?
    @State private var failure: String?
    @State private var attempt = 0
    @State private var opening = false

    /// From the bridge's width/height, else the decoded image, else 4:3 until it lands.
    private var aspect: CGFloat {
        if let ratio = image.aspectRatio { return CGFloat(ratio) }
        if let thumbnail, thumbnail.size.height > 0 { return thumbnail.size.width / thumbnail.size.height }
        return 4 / 3
    }

    var body: some View {
        Button(action: open) {
            ZStack {
                Color(.secondarySystemFill)
                if let thumbnail {
                    // A row tile clamps very tall or wide images; it fills (the viewer shows the whole image).
                    Image(uiImage: thumbnail).resizable().aspectRatio(contentMode: fixedHeight ? .fill : .fit)
                } else if let failure {
                    VStack(spacing: 4) {
                        Image(systemName: "photo.badge.exclamationmark")
                        Text(failure).font(.caption2)
                    }
                    .foregroundStyle(.secondary)
                } else {
                    ProgressView()
                }
            }
            .modifier(AspectSlot(aspect: aspect, maxHeight: maxHeight, fixedHeight: fixedHeight))
            .clipShape(.rect(cornerRadius: 12))
            .overlay { RoundedRectangle(cornerRadius: 12).strokeBorder(.quaternary, lineWidth: 0.5) }
            .contentShape(.rect(cornerRadius: 12))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityAddTraits(.isImage)
        .accessibilityIdentifier("toolImage")
        .accessibilityValue(thumbnail != nil ? "loaded" : failure != nil ? "failed" : "loading")
        .task(id: attempt) { await fetch() }
    }

    private func fetch() async {
        guard thumbnail == nil else { return }
        failure = nil
        do {
            let data = try await load()
            let maxPixels = maxHeight * 3 * max(aspect, 1)
            thumbnail = await Task.detached { Self.downsample(data, maxPixels: maxPixels) }.value
            if thumbnail == nil { failure = "Can't show" }
        } catch RelayError.http(404, _, _) {
            onUnavailable()
        } catch RelayError.http(413, _, _) {
            failure = "Too large"
        } catch is CancellationError {
        } catch {
            failure = "Tap to retry"
        }
    }

    /// Decodes straight to the slot's size at 3x (ImageIO, never the full bitmap), keeping the aspect.
    nonisolated private static func downsample(_ data: Data, maxPixels: CGFloat) -> UIImage? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixels,
        ]
        guard let cg = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else { return nil }
        return UIImage(cgImage: cg)
    }

    private func open() {
        if failure != nil, thumbnail == nil {
            attempt += 1
            return
        }
        guard thumbnail != nil, !opening else { return }
        opening = true
        Task {
            defer { opening = false }
            guard let data = try? await load(),
                  let full = await Task.detached(operation: { UIImage(data: data) }).value else { return }
            onOpen(full)
        }
    }
}

/// Sizes a tile to `aspect` before its bytes land, so nothing jumps: full width up to `maxHeight`, or
/// exactly `maxHeight` tall in a row.
private struct AspectSlot: ViewModifier {
    let aspect: CGFloat
    let maxHeight: CGFloat
    let fixedHeight: Bool

    func body(content: Content) -> some View {
        if fixedHeight {
            content.frame(width: min(max(maxHeight * aspect, 90), 320), height: maxHeight)
        } else {
            AspectFitLayout(aspect: aspect, maxHeight: maxHeight) { content }
        }
    }
}

private struct AspectFitLayout: Layout {
    let aspect: CGFloat
    let maxHeight: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let limit = maxHeight * aspect
        let width = min(proposal.width ?? limit, limit)
        return CGSize(width: width, height: width / aspect)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for subview in subviews {
            subview.place(at: bounds.origin, proposal: ProposedViewSize(bounds.size))
        }
    }
}
