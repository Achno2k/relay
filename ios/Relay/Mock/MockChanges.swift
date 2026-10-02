#if DEBUG
import Foundation
import RelayKit

/// Made-up git state behind the Changes screen (`GET /agents/:id/changes*`), keyed by the agent's workspace.
/// website: a busy branch; shop-api p1: clean and pushed; shop-api p2: a branch with no upstream; others: no repo.
enum MockChanges {
    private static let now = Date()

    private static func ago(_ minutes: Double) -> Date { now.addingTimeInterval(-minutes * 60) }

    static func changes(agentId: String, workspaceName: String) -> Changes {
        switch (workspaceName, MachineKey.raw(agentId)) {
        case ("website", _):
            Changes(
                repo: true, branch: "hero-redesign", upstream: "origin/hero-redesign", ahead: 2, behind: 1,
                files: websiteFiles.map(\.file), commits: websiteCommits.map(\.summary)
            )
        case ("shop-api", "w1:p1"):
            Changes(repo: true, branch: "main", upstream: "origin/main", ahead: 0, behind: 0)
        case ("shop-api", _):
            Changes(
                repo: true, branch: "fix-flaky-checkout",
                files: shopFiles.map(\.file), commits: shopCommits.map(\.summary)
            )
        default:
            Changes(repo: false)
        }
    }

    static func diff(agentId: String, workspaceName: String, path: String) throws -> FileDiffText {
        if path.isEmpty || path.hasPrefix("/") { throw RelayError.http(status: 400, code: "bad_request", message: "path must be relative") }
        if path.split(separator: "/").contains("..") {
            throw RelayError.http(status: 403, code: "forbidden", message: "path is outside the agent's folder")
        }
        let files = workspaceName == "website" ? websiteFiles : workspaceName == "shop-api" && MachineKey.raw(agentId) != "w1:p1" ? shopFiles : []
        guard let entry = files.first(where: { $0.file.path == path }) else {
            throw RelayError.http(status: 404, code: "not_found", message: "no uncommitted change at that path")
        }
        return FileDiffText(path: path, diff: entry.diff, binary: entry.file.binary)
    }

    static func commit(agentId: String, workspaceName: String, sha: String) throws -> CommitDetail {
        let commits = workspaceName == "website" ? websiteCommits : workspaceName == "shop-api" ? shopCommits : []
        guard let commit = commits.first(where: { $0.summary.sha.hasPrefix(sha) }) else {
            throw RelayError.http(status: 404, code: "not_found", message: "no such commit")
        }
        return CommitDetail(
            sha: commit.summary.sha, shortSha: commit.summary.shortSha, subject: commit.summary.subject,
            body: commit.body, time: commit.summary.time, files: commit.files
        )
    }

    // MARK: - website

    private static let websiteFiles: [(file: ChangedFile, diff: String)] = [
        (ChangedFile(path: "assets/hero.png", status: .modified, binary: true), ""),
        (ChangedFile(path: "docs/legacy/intro.md", status: .deleted, deletions: 4), """
        diff --git a/docs/legacy/intro.md b/docs/legacy/intro.md
        deleted file mode 100644
        --- a/docs/legacy/intro.md
        +++ /dev/null
        @@ -1,4 +0,0 @@
        -# Getting started (legacy)
        -
        -This guide covers the old checkout widget.
        -Use the new guide instead.

        """),
        (ChangedFile(path: "README.md", status: .modified, additions: 1, deletions: 0), """
        --- a/README.md
        +++ b/README.md
        @@ -14,3 +14,4 @@ npm run dev
         - `src/components`: page sections (`Hero`, `Pricing`, `Footer`)
         - `src/styles`: one file per section, all spacing from `tokens.css`
         - `tests/checkout`: pytest suite for the payment flow
        +- `src/components/Banner.tsx`: the promo strip above the hero

        """),
        (ChangedFile(path: "src/components/Banner.tsx", oldPath: "src/components/Promo.tsx", status: .renamed), ""),
        (ChangedFile(path: "src/components/Hero.tsx", status: .modified, additions: 2, deletions: 2), """
        --- a/src/components/Hero.tsx
        +++ b/src/components/Hero.tsx
        @@ -3,10 +3,10 @@ import { Button } from "./Button";
         export function Hero() {
           return (
             <section className="hero">
        -      <h1>The all-in-one commerce platform that handles payments, tax, shipping and more for you.</h1>
        +      <h1>Ship the storefront, not the plumbing.</h1>
               <p className="lede">Payments, tax and shipping, wired up before lunch.</p>
               <Button href="/signup" size="lg">
        -        Get started
        +        Start free
               </Button>
             </section>
           );

        """),
        (ChangedFile(path: "src/styles/section3.css", status: .untracked, additions: 12), """
        --- /dev/null
        +++ b/src/styles/section3.css
        @@ -0,0 +1,12 @@
        +.section-3 {
        +  display: grid;
        +  gap: var(--space-8);
        +  padding-block: var(--space-12);
        +}
        +
        +@media (max-width: 640px) {
        +  .section-3 {
        +    gap: var(--space-6);
        +  }
        +}
        +

        """),
    ]

    private static let websiteCommits: [(summary: CommitSummary, body: String, files: [CommitFile])] = [
        (
            CommitSummary(
                sha: "4f1c2a9e7b3d5c8a0e6f2b4d9c1a7e3f5b8d0c26", shortSha: "4f1c2a9",
                subject: "hero: tighter headline and a single call to action", time: ago(38),
                fileCount: 2, additions: 4, deletions: 4
            ),
            "The old headline wrapped to four lines on small phones.\nOne button instead of two; the secondary link moves to the footer.",
            [
                CommitFile(file: ChangedFile(path: "src/components/Hero.tsx", status: .modified, additions: 3, deletions: 4), diff: """
                --- a/src/components/Hero.tsx
                +++ b/src/components/Hero.tsx
                @@ -8,9 +8,8 @@ export function Hero() {
                       <p className="lede">Payments, tax and shipping, wired up before lunch.</p>
                -      <div className="actions">
                -        <Button href="/signup" size="lg">Get started</Button>
                +      <Button href="/signup" size="lg">
                +        Get started
                -        <Button href="/docs" variant="ghost">Read the docs</Button>
                -      </div>
                +      </Button>
                     </section>

                """),
                CommitFile(file: ChangedFile(path: "src/components/Footer.tsx", status: .modified, additions: 1, deletions: 0), diff: """
                --- a/src/components/Footer.tsx
                +++ b/src/components/Footer.tsx
                @@ -6,3 +6,4 @@ export function Footer() {
                       <a href="/pricing">Pricing</a>
                +      <a href="/docs">Docs</a>
                       <a href="/contact">Contact</a>

                """),
            ]
        ),
        (
            CommitSummary(
                sha: "9b7e0d3c1f5a2e8b6d4c0a9f7e3b1d5c8a2f6e04", shortSha: "9b7e0d3",
                subject: "styles: spacing tokens for every section", time: ago(60 * 26),
                fileCount: 1, additions: 1, deletions: 1
            ),
            "",
            [
                CommitFile(file: ChangedFile(path: "src/styles/section1.css", status: .modified, additions: 1, deletions: 1), diff: """
                --- a/src/styles/section1.css
                +++ b/src/styles/section1.css
                @@ -1,5 +1,5 @@
                 .section-1 {
                   display: grid;
                -  gap: 48px;
                +  gap: var(--space-8);
                   padding-block: var(--space-12);
                 }

                """),
            ]
        ),
    ]

    // MARK: - shop-api

    private static let shopFiles: [(file: ChangedFile, diff: String)] = [
        (ChangedFile(path: "tests/checkout/conftest.py", status: .modified, additions: 3, deletions: 2), """
        --- a/tests/checkout/conftest.py
        +++ b/tests/checkout/conftest.py
        @@ -12,6 +12,7 @@ def db():


         @pytest.fixture
        -def cart(db):
        -    return Cart.shared(db)
        +def cart(db, request):
        +    # One cart per test: test_pay.py mutates it, and xdist can share a worker.
        +    return Cart.create(db, owner=f"test-{request.node.name}")

        """),
        (ChangedFile(path: "tests/checkout/test_refund.py", status: .conflicted, additions: 5, deletions: 0), """
        --- a/tests/checkout/test_refund.py
        +++ b/tests/checkout/test_refund.py
        @@ -4,3 +4,8 @@ from checkout.refund import refund
         def test_refund_full(cart):
        +<<<<<<< HEAD
        +    assert refund(cart).amount == cart.total
        +=======
        +    assert refund(cart, partial=False).amount == cart.total
        +>>>>>>> origin/main

        """),
    ]

    private static let shopCommits: [(summary: CommitSummary, body: String, files: [CommitFile])] = [
        (
            CommitSummary(
                sha: "c03e5a7b9d1f2c4e6a8b0d2f4c6e8a0b2d4f6a81", shortSha: "c03e5a7",
                subject: "checkout: retry the payment webhook once on a 502", time: ago(12),
                fileCount: 1, additions: 4, deletions: 1
            ),
            "",
            [
                CommitFile(file: ChangedFile(path: "checkout/webhooks.py", status: .modified, additions: 4, deletions: 1), diff: """
                --- a/checkout/webhooks.py
                +++ b/checkout/webhooks.py
                @@ -20,4 +20,7 @@ def deliver(event):
                     resp = client.post(event.url, json=event.payload, timeout=5)
                -    resp.raise_for_status()
                +    if resp.status_code == 502:
                +        resp = client.post(event.url, json=event.payload, timeout=5)
                +    resp.raise_for_status()
                +    return resp

                """),
            ]
        ),
    ]
}
#endif
