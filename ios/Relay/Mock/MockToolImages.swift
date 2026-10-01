#if DEBUG
import RelayKit
import UIKit

/// Images returned by tools in the mock chats. Generated, so no binary is checked in.
enum MockToolImages {
    /// `l-t3`: Read assets/hero.png (the same banner the file viewer shows).
    static let hero = [ToolImage(index: 0, mediaType: "image/png", bytes: 48_213, width: 600, height: 320)]
    /// `l-t5`: two phone screenshots, light and dark.
    static let screens = [
        ToolImage(index: 0, mediaType: "image/png", bytes: 61_402, width: 390, height: 844),
        ToolImage(index: 1, mediaType: "image/png", bytes: 59_877, width: 390, height: 844),
    ]

    static func data(toolCallId: String, index: Int) -> Data? {
        switch (toolCallId, index) {
        case ("l-t3", 0): MockFiles.heroImage()
        case ("l-t5", 0), ("l-t5", 1): phone(dark: index == 1)
        default: nil
        }
    }

    /// A made-up storefront screen: a header bar, the hero and a few rows.
    private static func phone(dark: Bool) -> Data? {
        let size = CGSize(width: 390, height: 844)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: size, format: format).pngData { ctx in
            (dark ? UIColor.black : UIColor.white).setFill()
            ctx.fill(CGRect(origin: .zero, size: size))
            let ink = dark ? UIColor.white : UIColor.black
            ("Storefront" as NSString).draw(at: CGPoint(x: 24, y: 70), withAttributes: [
                .font: UIFont.systemFont(ofSize: 30, weight: .bold), .foregroundColor: ink,
            ])
            UIColor.systemIndigo.setFill()
            UIBezierPath(roundedRect: CGRect(x: 24, y: 130, width: 342, height: 200), cornerRadius: 20).fill()
            ("Ship the storefront" as NSString).draw(at: CGPoint(x: 44, y: 280), withAttributes: [
                .font: UIFont.systemFont(ofSize: 22, weight: .bold), .foregroundColor: UIColor.white,
            ])
            (dark ? UIColor.darkGray : UIColor.systemGray5).setFill()
            for row in 0..<5 {
                UIBezierPath(roundedRect: CGRect(x: 24, y: 360 + CGFloat(row) * 76, width: 342, height: 60), cornerRadius: 14).fill()
            }
            UIColor.systemBlue.setFill()
            UIBezierPath(roundedRect: CGRect(x: 24, y: 760, width: 342, height: 52), cornerRadius: 26).fill()
            ("Start free" as NSString).draw(at: CGPoint(x: 152, y: 774), withAttributes: [
                .font: UIFont.systemFont(ofSize: 18, weight: .semibold), .foregroundColor: UIColor.white,
            ])
        }
    }
}
#endif
