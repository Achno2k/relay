import RelayKit
import SwiftUI

/// Leading status glyph. Monochrome: the shape carries the state, not a colour.
/// spinner = working, hand = needs input, eye = ready for review, circle+check = completed, circle = idle.
struct StatusGlyph: View {
    let status: AgentStatus
    let unseen: Bool
    /// The glyph's box; symbols scale with Dynamic Type from this.
    @ScaledMetric(relativeTo: .body) private var size: CGFloat = 18

    var body: some View {
        Group {
            switch status {
            case .working:
                Spinner()
            case .blocked:
                Image(systemName: "hand.raised").foregroundStyle(.primary)
            case .done where unseen:
                Image(systemName: "eye").foregroundStyle(.primary)
            case .done:
                Image(systemName: "checkmark.circle").foregroundStyle(.secondary)
            case .idle:
                Image(systemName: "circle").foregroundStyle(.secondary)
            case .unknown:
                Image(systemName: "circle.dashed").foregroundStyle(.tertiary)
            }
        }
        .font(.system(size: size * 0.95, weight: .medium))
        .frame(width: size, height: size)
        .accessibilityLabel(Self.label(status, unseen: unseen))
    }

    static func label(_ status: AgentStatus, unseen: Bool) -> String {
        switch status {
        case .working: "Working"
        case .blocked: "Needs input"
        case .done: unseen ? "Ready for review" : "Completed"
        case .idle: "Idle"
        case .unknown: "Unknown"
        }
    }
}

/// The iOS activity spinner drawn in the label colour (a tinted `ProgressView` stays grey): eight spokes,
/// stepping round every 0.9 s. Holds still under Reduce Motion.
struct Spinner: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private static let opacities: [Double] = [1, 0.85, 0.7, 0.57, 0.45, 0.35, 0.26, 0.18]

    var body: some View {
        TimelineView(.periodic(from: .now, by: 0.9 / 8)) { context in
            let step = reduceMotion ? 0 : Int(context.date.timeIntervalSinceReferenceDate / (0.9 / 8)) % 8
            Canvas { ctx, size in
                let unit = min(size.width, size.height) / 20
                let center = CGPoint(x: size.width / 2, y: size.height / 2)
                for i in 0..<8 {
                    // Spoke i trails the head by i steps, counter-clockwise, as in the design.
                    let angle = Double(step - i) * .pi / 4
                    var spoke = Path()
                    spoke.move(to: CGPoint(x: 0, y: -8 * unit))
                    spoke.addLine(to: CGPoint(x: 0, y: -4.5 * unit))
                    let t = CGAffineTransform(translationX: center.x, y: center.y).rotated(by: angle)
                    ctx.stroke(
                        spoke.applying(t),
                        with: .color(Color.primary.opacity(Self.opacities[i])),
                        style: StrokeStyle(lineWidth: 2.2 * unit, lineCap: .round)
                    )
                }
            }
        }
    }
}
