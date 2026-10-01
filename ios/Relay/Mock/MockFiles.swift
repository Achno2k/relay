#if DEBUG
import Foundation
import RelayKit
import UIKit

/// Made-up project files behind the mock chats' Read/Write/Edit rows (`GET /agents/:id/file`).
enum MockFiles {
    static let conftest = """
    import pytest

    from checkout.cart import Cart
    from checkout.db import session


    @pytest.fixture
    def db():
        with session() as s:
            yield s
            s.rollback()


    @pytest.fixture
    def cart(db, request):
        # One cart per test: test_pay.py mutates it, and xdist can share a worker.
        return Cart.create(db, owner=f"test-{request.node.name}")

    """

    static let conftestEdit = ToolEdit(kind: .edit, changes: [
        ToolEditChange(
            old: "@pytest.fixture\ndef cart(db):\n    return Cart.shared(db)",
            new: "@pytest.fixture\ndef cart(db, request):\n    # One cart per test: test_pay.py mutates it, and xdist can share a worker.\n    return Cart.create(db, owner=f\"test-{request.node.name}\")",
            replaceAll: false
        ),
    ])

    static let hero = """
    import { Button } from "./Button";

    export function Hero() {
      return (
        <section className="hero">
          <h1>Ship the storefront, not the plumbing.</h1>
          <p className="lede">Payments, tax and shipping, wired up before lunch.</p>
          <Button href="/signup" size="lg">
            Start free
          </Button>
        </section>
      );
    }

    """

    static let heroEdit = ToolEdit(kind: .edit, changes: [
        ToolEditChange(
            old: "      <h1>The all-in-one commerce platform that handles payments, tax, shipping and more for you.</h1>",
            new: "      <h1>Ship the storefront, not the plumbing.</h1>",
            replaceAll: false
        ),
        ToolEditChange(old: "        Get started", new: "        Start free", replaceAll: false),
    ])

    static let readme = """
    # Storefront

    The marketing site and checkout for the demo shop.

    ## Run it

    ```sh
    npm install
    npm run dev
    ```

    ## Layout

    - `src/components`: page sections (`Hero`, `Pricing`, `Footer`)
    - `src/styles`: one file per section, all spacing from `tokens.css`
    - `tests/checkout`: pytest suite for the payment flow

    """

    static func section(_ n: Int) -> String {
        """
        .section-\(n) {
          display: grid;
          gap: var(--space-8);
          padding-block: var(--space-12);
        }

        @media (max-width: 640px) {
          .section-\(n) {
            gap: var(--space-6);
          }
        }

        """
    }

    static let sectionEdit = ToolEdit(kind: .edit, changes: [
        ToolEditChange(old: "  gap: 48px;", new: "  gap: var(--space-8);", replaceAll: false),
    ])

    /// codex-style: one unified diff over two files, no single path.
    static let legacyDiff = ToolEdit(kind: .diff, diff: """
    --- a/docs/index.md
    +++ b/docs/index.md
    @@ -3,4 +3,3 @@
     ## Guides
    -- [Getting started (legacy)](legacy/intro.md)
     - [Getting started](guides/start.md)
     - [Deploying](guides/deploy.md)
    --- a/mkdocs.yml
    +++ b/mkdocs.yml
    @@ -12,3 +12,2 @@ nav:
       - Guides: guides/start.md
    -  - Legacy: legacy/intro.md
       - Deploy: guides/deploy.md

    """)

    static func text(_ path: String) -> String? {
        switch path {
        case "tests/checkout/conftest.py": conftest
        case "src/components/Hero.tsx": hero
        case "README.md": readme
        default:
            if path.hasPrefix("src/styles/section"), let n = Int(path.filter(\.isNumber)) { section(n) } else { nil }
        }
    }

    static func language(_ path: String) -> String? {
        switch (path as NSString).pathExtension {
        case "py": "py"
        case "tsx": "tsx"
        case "md": "md"
        case "css": "css"
        default: nil
        }
    }

    /// Serves `path` like the bridge would, errors included.
    static func file(_ path: String) throws -> AgentFile {
        if path.hasPrefix("/") || path.hasPrefix("~") { throw RelayError.http(status: 400, code: "bad_request", message: "path must be relative") }
        if path.split(separator: "/").contains("..") {
            throw RelayError.http(status: 403, code: "forbidden", message: "path is outside the agent's folder")
        }
        if path == "assets/hero.png", let png = heroImage() { return .image(png, contentType: "image/png") }
        if path.hasSuffix(".bin") { throw RelayError.http(status: 415, code: "unsupported", message: "not a text file") }
        guard let content = text(path) else { throw RelayError.http(status: 404, code: "not_found", message: "no such file") }
        return .text(FileContent(path: path, content: content, size: content.utf8.count, language: language(path)))
    }

    /// A small generated banner, so no binary is checked in.
    static func heroImage() -> Data? {
        let size = CGSize(width: 600, height: 320)
        return UIGraphicsImageRenderer(size: size).pngData { ctx in
            let colors = [UIColor.systemIndigo.cgColor, UIColor.systemTeal.cgColor] as CFArray
            if let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: colors, locations: [0, 1]) {
                ctx.cgContext.drawLinearGradient(gradient, start: .zero, end: CGPoint(x: size.width, y: size.height), options: [])
            }
            let title = "Ship the storefront" as NSString
            title.draw(at: CGPoint(x: 40, y: 130), withAttributes: [
                .font: UIFont.systemFont(ofSize: 40, weight: .bold), .foregroundColor: UIColor.white,
            ])
        }
    }
}
#endif
