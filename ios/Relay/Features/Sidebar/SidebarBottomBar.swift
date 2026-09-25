import SwiftUI

/// Search is a 50 pt glass circle bottom-left that grows into a field; New chat is the label-coloured
/// capsule on the right. Both sit where a thumb reaches, and the list keeps its full height for chats.
struct SidebarBottomBar: View {
    @Binding var query: String
    @Binding var searching: Bool
    var searchFocused: FocusState<Bool>.Binding
    /// New chat shows only its compose icon (from AX3 up the title no longer fits beside search on one line).
    /// Passed in because the bar caps its own type size at AX1.
    var iconOnly = false
    let onNewChat: () -> Void

    @Namespace private var glass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Reduce Motion: the circle swaps for the field without the glass morph.
    private var morph: Animation? { reduceMotion ? nil : .smooth }

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                if searching {
                    searchField
                } else {
                    searchButton
                    Spacer(minLength: 0)
                    newChatButton
                }
            }
        }
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            TextField("Search chats", text: $query)
                .focused(searchFocused)
                .submitLabel(.search)
                .accessibilityIdentifier("sidebarSearchField")
            Button {
                withAnimation(morph) {
                    query = ""
                    searching = false
                    searchFocused.wrappedValue = false
                }
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.tertiary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Close search")
        }
        .padding(.leading, 18)
        .padding(.trailing, 4)
        .frame(minHeight: 50)
        .glassEffect(.regular.interactive(), in: .capsule)
        .glassEffectID("search", in: glass)
    }

    private var searchButton: some View {
        Button {
            withAnimation(morph) { searching = true }
            searchFocused.wrappedValue = true
        } label: {
            Image(systemName: "magnifyingglass")
                .font(.title3.weight(.medium))
                .frame(width: 50, height: 50)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: .circle)
        .glassEffectID("search", in: glass)
        .accessibilityLabel("Search")
        .accessibilityIdentifier("sidebarSearch")
    }

    private var newChatButton: some View {
        Button(action: onNewChat) {
            Label("New chat", systemImage: "square.and.pencil")
                .font(.body.weight(.semibold))
                .lineLimit(1)
                .labelStyle(NewChatLabelStyle(iconOnly: iconOnly))
                .foregroundStyle(Color(.systemBackground))
                .padding(.leading, iconOnly ? 20 : 18)
                .padding(.trailing, iconOnly ? 20 : 22)
                .frame(minHeight: 50)
                .contentShape(.capsule)
        }
        .accessibilityLabel("New chat")
        .buttonStyle(.plain)
        .glassEffect(.regular.tint(.primary).interactive(), in: .capsule)
        .glassEffectID("new", in: glass)
        .accessibilityIdentifier("sidebarNewChat")
    }
}

/// Icon and title 8 pt apart, as in the mockup (the default label spacing is wider).
private struct NewChatLabelStyle: LabelStyle {
    var iconOnly = false

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 8) {
            configuration.icon
            if !iconOnly { configuration.title }
        }
    }
}
