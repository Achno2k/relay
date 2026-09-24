import CoreImage
import Foundation
import Testing
@testable import RelayCore

@Suite struct MiscTests {
    @Test func parsesHerdrEvents() {
        let a = HerdrEvent.parse(Data(#"{"event":"pane_agent_status_changed","data":{"type":"pane_agent_status_changed","pane_id":"w1:p1","workspace_id":"w1","agent_status":"working"}}"#.utf8))
        #expect(a == HerdrEvent(kind: "pane.agent.status.changed", paneId: "w1:p1"))
        let b = HerdrEvent.parse(Data(#"{"event":"pane_created","data":{"type":"pane_created","pane":{"pane_id":"w1:p9"}}}"#.utf8))
        #expect(b?.paneId == "w1:p9")
        #expect(HerdrEvent.parse(Data(#"{"id":"x","result":{"type":"subscription_started"}}"#.utf8)) == nil)
    }

    @Test func pairingURL() {
        #expect(Pairing.url(host: "100.64.0.1", port: 7878, token: "abc-_1") == "relay://pair?url=http%3A%2F%2F100.64.0.1%3A7878&token=abc-_1")
    }

    @Test func tokenIsURLSafe() {
        let t = TokenStore.generate()
        #expect(t.count == 43)
        #expect(t.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" })
    }

    @Test func projectDirName() {
        #expect(TranscriptLocator.projectDirName(for: "/Users/dev/my.app_v2") == "-Users-dev-my-app-v2")
    }

    @Test func agentNames() {
        #expect(AgentService.isValidName("api-refactor"))
        #expect(!AgentService.isValidName("Api"))
        #expect(!AgentService.isValidName("1abc"))
    }

    @Test func timestamps() {
        #expect(Timestamps.normalize("2026-09-23T13:16:54.975Z") == "2026-09-23T13:16:54+00:00")
        #expect(Timestamps.normalize("2026-09-23T13:16:54Z") == "2026-09-23T13:16:54+00:00")
        #expect(Timestamps.format(Date(timeIntervalSince1970: 0)) == "1970-01-01T00:00:00+00:00")
    }

    @Test func qrRoundTrips() throws {
        let text = "relay://pair?url=http%3A%2F%2F100.64.0.1%3A7878&token=abc"
        let m = try #require(QRCode.modules(for: text))
        // Re-render the matrix at 8px per module with a quiet zone and decode it.
        let scale = 8, quiet = 4, n = m.count
        let size = (n + quiet * 2) * scale
        var px = [UInt8](repeating: 255, count: size * size)
        for (y, row) in m.enumerated() {
            for (x, dark) in row.enumerated() where dark {
                for dy in 0..<scale {
                    for dx in 0..<scale { px[((y + quiet) * scale + dy) * size + (x + quiet) * scale + dx] = 0 }
                }
            }
        }
        let image = CIImage(bitmapData: Data(px), bytesPerRow: size, size: CGSize(width: size, height: size), format: .L8, colorSpace: CGColorSpaceCreateDeviceGray())
        let detector = try #require(CIDetector(ofType: CIDetectorTypeQRCode, context: nil, options: nil))
        let decoded = detector.features(in: image).compactMap { ($0 as? CIQRCodeFeature)?.messageString }
        #expect(decoded == [text])
        #expect(QRCode.terminal(text)?.contains("▀") == true)
    }

    @Test func launchdPlist() throws {
        let data = try LaunchAgent.plist(executable: "/usr/local/bin/relay", port: 7878)
        let p = try #require(try PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any])
        #expect(p["ProgramArguments"] as? [String] == ["/usr/local/bin/relay", "serve", "--port", "7878", "--require-tailscale"])
    }
}
