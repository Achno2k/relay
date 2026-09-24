import HerdKit
import SwiftUI
import VisionKit

/// First launch: scan the QR from `herd pair` or type the URL and token.
struct PairingView: View {
    @Environment(AppModel.self) private var model
    @State private var scanning = false
    @State private var manual = false

    var body: some View {
        VStack(spacing: 0) {
            Spacer()
            HerdMark()
                .padding(.bottom, 28)
            Text("Herd")
                .font(.largeTitle.weight(.bold))
            Text("Your coding agents, in your pocket.")
                .font(.title3)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.top, 6)
            Spacer()

            VStack(alignment: .leading, spacing: 18) {
                Step(number: 1, text: "On your Mac, run **herd serve**.")
                Step(number: 2, text: "Then run **herd pair** and scan the code it shows.")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 32)
            .padding(.bottom, 32)

            if let error = model.pairingError {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .padding(.horizontal, 24)
                    .padding(.bottom, 12)
            }

            VStack(spacing: 12) {
                Button {
                    scanning = true
                } label: {
                    Label("Scan pairing code", systemImage: "qrcode.viewfinder")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                }
                .buttonStyle(.glassProminent)

                Button {
                    manual = true
                } label: {
                    Text("Enter manually")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                }
                .buttonStyle(.glass)
            }
            .controlSize(.large)
            .disabled(model.isPairing)
            .padding(.horizontal, 24)
            .padding(.bottom, 12)
        }
        .overlay {
            if model.isPairing {
                ProgressView("Connecting…")
                    .padding(24)
                    .glassEffect(.regular, in: .rect(cornerRadius: 24))
            }
        }
        .fullScreenCover(isPresented: $scanning) {
            ScannerScreen { link in
                scanning = false
                model.handle(url: link)
            }
        }
        .sheet(isPresented: $manual) {
            ManualPairingSheet()
        }
    }
}

private struct Step: View {
    let number: Int
    let text: LocalizedStringKey

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 14) {
            Text("\(number)")
                .font(.footnote.weight(.bold))
                .frame(width: 24, height: 24)
                .background(Color(.tertiarySystemFill), in: .circle)
            Text(text)
                .foregroundStyle(.secondary)
        }
    }
}

/// Three overlapping glass bubbles: agents, grouped.
private struct HerdMark: View {
    var body: some View {
        GlassEffectContainer(spacing: 20) {
            ZStack {
                bubble("sparkle", size: 76).offset(x: -34, y: 14)
                bubble("chevron.left.forwardslash.chevron.right", size: 64).offset(x: 40, y: 20)
                bubble("bubble.left.fill", size: 92).offset(y: -22)
            }
        }
        .frame(width: 180, height: 150)
    }

    private func bubble(_ symbol: String, size: CGFloat) -> some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.36, weight: .semibold))
            .foregroundStyle(.tint)
            .frame(width: size, height: size)
            .glassEffect(.regular.tint(.accentColor.opacity(0.15)), in: .circle)
    }
}

private struct ManualPairingSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var url = ""
    @State private var token = ""

    private var pairing: Pairing? {
        // A pasted herd:// link works in the URL field too.
        if let link = Pairing(linkString: url) { return link }
        guard let base = Pairing.baseURL(from: url), !token.isEmpty else { return nil }
        return Pairing(url: base, token: token.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("http://100.64.0.1:7878", text: $url)
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } header: {
                    Text("Bridge URL")
                } footer: {
                    Text("Your Mac's Tailscale address. Pasting the whole herd:// link also works.")
                }
                Section("Token") {
                    SecureField("Token", text: $token)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
                if let error = model.pairingError {
                    Section {
                        Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("Enter manually")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(role: .confirm) {
                        guard let pairing else { return }
                        Task {
                            await model.pair(pairing)
                            if model.pairingError == nil { dismiss() }
                        }
                    } label: {
                        if model.isPairing { ProgressView() } else { Label("Connect", systemImage: "checkmark") }
                    }
                    .disabled(pairing == nil || model.isPairing)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

private struct ScannerScreen: View {
    let onScan: (URL) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ZStack {
            if DataScannerViewController.isSupported && DataScannerViewController.isAvailable {
                QRScanner(onScan: onScan).ignoresSafeArea()
                RoundedRectangle(cornerRadius: 32)
                    .strokeBorder(.white.opacity(0.9), lineWidth: 3)
                    .frame(width: 250, height: 250)
            } else {
                Color.black.ignoresSafeArea()
                ContentUnavailableView(
                    "Camera unavailable",
                    systemImage: "camera.fill",
                    description: Text("Scanning needs a device camera and permission to use it. Enter the code manually instead.")
                )
                .foregroundStyle(.white)
            }
        }
        .overlay(alignment: .topTrailing) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.body.weight(.semibold))
                    .frame(width: 44, height: 44)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.white)
            .glassEffect(.regular.interactive(), in: .circle)
            .padding()
            .accessibilityLabel("Close")
        }
        .overlay(alignment: .bottom) {
            Text("Point at the code from **herd pair**")
                .font(.subheadline)
                .foregroundStyle(.white)
                .padding(.horizontal, 18)
                .padding(.vertical, 10)
                .glassEffect(.regular, in: .capsule)
                .padding(.bottom, 40)
        }
        .preferredColorScheme(.dark)
    }
}

private struct QRScanner: UIViewControllerRepresentable {
    let onScan: (URL) -> Void

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            qualityLevel: .balanced,
            isHighlightingEnabled: true
        )
        scanner.delegate = context.coordinator
        try? scanner.startScanning()
        return scanner
    }

    func updateUIViewController(_ controller: DataScannerViewController, context: Context) {}

    static func dismantleUIViewController(_ controller: DataScannerViewController, coordinator: Coordinator) {
        controller.stopScanning()
    }

    func makeCoordinator() -> Coordinator { Coordinator(onScan: onScan) }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        let onScan: (URL) -> Void
        private var done = false

        init(onScan: @escaping (URL) -> Void) {
            self.onScan = onScan
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            for item in addedItems {
                guard !done, case .barcode(let code) = item,
                      let payload = code.payloadStringValue,
                      let url = URL(string: payload), Pairing(link: url) != nil
                else { continue }
                done = true
                UINotificationFeedbackGenerator().notificationOccurred(.success)
                onScan(url)
            }
        }
    }
}
