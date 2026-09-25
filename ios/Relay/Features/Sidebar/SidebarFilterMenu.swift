import SwiftUI

/// The toolbar's Filter button and its status menu. It sits inside the shell's glass pill, so it has no
/// glass of its own. The native iOS 26 menu gives the glass card, checkmark column and dimming the design
/// draws in HTML. A dot on the button means a chat is waiting for input.
struct SidebarFilterMenu: View {
    @Binding var selection: SessionFilter
    let needsInput: Bool

    var body: some View {
        Menu {
            // Checkmark toggles, not a Picker: menus drop separators inside an inline Picker, and the
            // design sets Archived apart.
            Section {
                ForEach(SessionFilter.allCases.filter { $0 != .archived }, content: item)
            }
            Section {
                item(.archived)
            }
        } label: {
            Image(systemName: "line.3.horizontal.decrease")
                .font(.body.weight(.medium))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
                .overlay(alignment: .topTrailing) {
                    if needsInput {
                        Circle().fill(.primary).frame(width: 7, height: 7).offset(x: -9, y: 9)
                    }
                }
        }
        .tint(.primary)
        .accessibilityLabel("Filter: \(selection.title)")
        .accessibilityValue(needsInput ? "needs input" : "")
        .accessibilityIdentifier("sidebarFilter")
    }

    private func item(_ filter: SessionFilter) -> some View {
        Toggle(isOn: Binding(get: { selection == filter }, set: { if $0 { selection = filter } })) {
            Label { Text(filter.title) } icon: { Image(uiImage: Self.icon(filter)) }
        }
    }

    /// The design's glyphs. UIKit menus mark a row Selected when its image is named "checkmark…", so
    /// VoiceOver would read Completed as chosen; the image gets its own identifier instead.
    private static func icon(_ filter: SessionFilter) -> UIImage {
        let image = UIImage(systemName: filter == .all ? "list.bullet" : filter.symbol) ?? UIImage()
        image.accessibilityIdentifier = "filter.\(filter.rawValue)"
        return image
    }
}
