import SwiftUI
import UIKit

/// Full-screen image: pinch or double-tap to zoom, drag to pan while zoomed, swipe down to close, share.
struct ImageViewer: View {
    let image: UIImage
    let name: String
    @Environment(\.dismiss) private var dismiss
    @State private var scale: CGFloat = 1
    @GestureState private var pinch: CGFloat = 1
    @State private var offset: CGSize = .zero
    @GestureState private var drag: CGSize = .zero

    private static let maxScale: CGFloat = 5
    private static let doubleTapScale: CGFloat = 2.5

    private var zoomed: Bool { scale > 1.01 }
    /// How far a swipe down has pulled the image at 1x, 0...1; fades the backdrop and shrinks the image.
    private var pull: CGFloat { zoomed ? 0 : min(max(drag.height, 0) / 400, 1) }

    var body: some View {
        GeometryReader { geo in
            let fitted = fittedSize(in: geo.size)
            ZStack {
                Color.black.opacity(1 - pull * 0.8)
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
                    .frame(width: fitted.width, height: fitted.height)
                    .scaleEffect(scale * pinch * (1 - pull * 0.25))
                    .offset(x: offset.width + drag.width, y: offset.height + drag.height)
                    .accessibilityLabel(name)
                    .accessibilityIdentifier("imageViewerImage")
                    .accessibilityValue(zoomed ? "zoomed" : "fit")
            }
            .frame(width: geo.size.width, height: geo.size.height)
            .contentShape(.rect)
            .gesture(magnify(fitted: fitted, in: geo.size).simultaneously(with: pan(fitted: fitted, in: geo.size)))
            .onTapGesture(count: 2, coordinateSpace: .local) { point in
                toggleZoom(at: point, fitted: fitted, in: geo.size)
            }
        }
        .ignoresSafeArea()
        .overlay(alignment: .top) { chrome.opacity(1 - pull) }
        .overlay(alignment: .bottom) {
            Text(name)
                .font(.footnote)
                .foregroundStyle(.white.opacity(0.8))
                .lineLimit(1)
                .padding(.horizontal, 24)
                .padding(.bottom, 12)
                .opacity(zoomed ? 0 : 1 - pull)
        }
        .presentationBackground(.clear)
        .environment(\.colorScheme, .dark)
        .statusBarHidden()
    }

    private var chrome: some View {
        HStack {
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark").font(.body.weight(.semibold)).frame(width: 44, height: 44)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: .circle)
            .accessibilityLabel("Close")
            .accessibilityIdentifier("imageViewerClose")
            Spacer()
            ShareLink(item: Image(uiImage: image), preview: SharePreview(name, image: Image(uiImage: image))) {
                Image(systemName: "square.and.arrow.up").font(.body.weight(.semibold)).frame(width: 44, height: 44)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: .circle)
            .accessibilityLabel("Share")
            .accessibilityIdentifier("imageViewerShare")
        }
        .foregroundStyle(.white)
        .padding(.horizontal)
        .padding(.top, 8)
    }

    private func magnify(fitted: CGSize, in container: CGSize) -> some Gesture {
        MagnifyGesture()
            .updating($pinch) { value, state, _ in state = value.magnification }
            .onEnded { value in
                withAnimation(.smooth) {
                    scale = min(max(scale * value.magnification, 1), Self.maxScale)
                    offset = clamped(offset, scale: scale, fitted: fitted, in: container)
                }
            }
    }

    /// Pans while zoomed; at 1x a swipe down past the threshold (or flung) closes the viewer.
    private func pan(fitted: CGSize, in container: CGSize) -> some Gesture {
        DragGesture(minimumDistance: 10)
            .updating($drag) { value, state, _ in state = value.translation }
            .onEnded { value in
                if zoomed {
                    let moved = CGSize(width: offset.width + value.translation.width, height: offset.height + value.translation.height)
                    withAnimation(.smooth) { offset = clamped(moved, scale: scale, fitted: fitted, in: container) }
                } else if value.translation.height > 120 || value.predictedEndTranslation.height > 400 {
                    dismiss()
                }
            }
    }

    /// Zooms in on the tapped point, or back out to fit.
    private func toggleZoom(at point: CGPoint, fitted: CGSize, in container: CGSize) {
        withAnimation(.smooth) {
            if zoomed {
                scale = 1
                offset = .zero
            } else {
                let s = Self.doubleTapScale
                // A point d from the centre lands at d*s after scaling; offsetting by d*(1-s) keeps it under the finger.
                let wanted = CGSize(
                    width: (container.width / 2 - point.x) * (s - 1),
                    height: (container.height / 2 - point.y) * (s - 1)
                )
                scale = s
                offset = clamped(wanted, scale: s, fitted: fitted, in: container)
            }
        }
    }

    private func fittedSize(in container: CGSize) -> CGSize {
        guard image.size.width > 0, image.size.height > 0, container.width > 0, container.height > 0 else { return container }
        let ratio = min(container.width / image.size.width, container.height / image.size.height)
        return CGSize(width: image.size.width * ratio, height: image.size.height * ratio)
    }

    /// Keeps the zoomed image covering the screen: no panning past its edges.
    private func clamped(_ offset: CGSize, scale: CGFloat, fitted: CGSize, in container: CGSize) -> CGSize {
        let maxX = max(0, (fitted.width * scale - container.width) / 2)
        let maxY = max(0, (fitted.height * scale - container.height) / 2)
        return CGSize(width: min(max(offset.width, -maxX), maxX), height: min(max(offset.height, -maxY), maxY))
    }
}
