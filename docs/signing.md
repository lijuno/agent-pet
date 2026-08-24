# Signing and notarizing a release

macOS refuses to run a downloaded app that Apple has not seen. Unsigned, the
bundle is quarantined and the user is told it *"is damaged and can't be
opened"* — which is not what has happened, and sends people looking for a fault
that does not exist.

The usual workaround is to strip the quarantine attribute. That is worse here
than in most projects, because [the plugin](../plugin/README.md) means an
**agent** installs this app. A person running `xattr -d` has at least seen the
Gatekeeper dialog and made a decision; an agent runs it without hesitating and
reports success. Notarizing removes the reason anyone would type it.

Everything below is a one-time setup, then two commands per release.

---

## One-time setup

### 1. Apple Developer Program

$99/year, [developer.apple.com/programs](https://developer.apple.com/programs/).

Enrol as an **Individual** unless you have a company: Organization enrolment
needs a D-U-N-S number and a legal entity and takes weeks. Enrolment is not
instant — expect up to 48 hours, and the portal reports *pending* until it
completes. Anything you try before then fails with unhelpful errors; Xcode's
certificate manager reports `The data couldn't be read because it isn't in the
correct format`, which says nothing about the cause.

Accept the Program License Agreement as soon as the membership is active.
An unaccepted agreement blocks certificate creation and API key creation, and
produces the same opaque errors.

**What becomes public.** The certificate carries your **legal name** and
**Team ID**, it ships inside every app you sign, and anyone can read it:

```bash
codesign -dvvv /Applications/agent-pet.app
```

macOS also shows the name in the Gatekeeper dialog on first launch — that is
the point of it. An Individual membership has no way to show a company name
instead. Code-signing certificates are *not* published to Certificate
Transparency logs, so it is not in a public searchable index; it is visible to
anyone holding the app.

Your **email is not in the certificate**. Apple takes only the public key from
the signing request and fills in every other field from your account. Verified
against shipped apps — each is `UID`, `CN`, `OU`, `O`, `C` and nothing else:

```
UID=X85ZX835W9, CN=Developer ID Application: Julien Ramseier (X85ZX835W9),
OU=X85ZX835W9, O=Julien Ramseier, C=CH
```

### 2. The certificate

You need exactly one, of exactly one type: **Developer ID Application**.
"Apple Development" and "Mac App Distribution" also sign, and both fail
notarization later with an error that does not mention the cause.
`scripts/notarize.sh` refuses to use them.

Do not use Xcode's certificate manager — it depends on account plumbing that
breaks in ways unrelated to signing. The portal path is deterministic.

**Generate the request on the machine that will sign.** This creates a private
key in that machine's keychain; a certificate whose private key lives elsewhere
cannot sign anything.

Keychain Access → menu **Certificate Assistant** → **Request a Certificate From
a Certificate Authority…**

| Field | Value |
|---|---|
| User Email Address | your Apple ID email — discarded by Apple |
| Common Name | your name; a label for the keypair |
| CA Email Address | leave empty |
| | tick **Saved to disk** and **Let me specify key pair information** |
| Key Size / Algorithm | 2048 bits, RSA |

Then at
[developer.apple.com/account/resources/certificates/add](https://developer.apple.com/account/resources/certificates/add):
choose **Developer ID Application**, pick the **G2 Sub-CA** if asked, upload the
`.certSigningRequest`, download the `.cer`, and double-click it to install.

```bash
security find-identity -v -p codesigning
```

You want `Developer ID Application: Your Name (TEAMID)`.

**Then export a backup immediately.** In Keychain Access, confirm a private key
sits under the certificate, select both, right-click → Export as `.p12` with a
password. Without the private key the certificate is inert, and only a small
number of Developer ID certificates may exist per account — so this file is not
a convenience. It is the only way to sign from a second machine, or from this
one after a reinstall.

**On a machine that is not the first one**, import that `.p12` instead of
repeating this section: double-click it, give the export password, and check
`security find-identity -v -p codesigning` again. Generating a second
certificate would spend one of the few the account may hold, to no end.

### 3. Notary credentials

Create an App Store Connect API key: Users and Access → Integrations → Keys,
**Developer** role. The `.p8` **downloads exactly once** and can never be
re-downloaded — keep it in a password manager. Losing it is recoverable only by
revoking the key and making another.

Store it in the keychain under a name:

```bash
xcrun notarytool store-credentials agent-pet --key ~/Downloads/AuthKey_XXXXX.p8 --key-id YOUR_KEY_ID
```

Add `--issuer <uuid>` **only for a Team API key**. Individual API keys must not
be given one — `notarytool` says so in its own help, and passing it anyway
fails.

This validates against Apple before saving (`--validate` defaults on), so a
successful run proves the credentials work. Confirm:

```bash
xcrun notarytool history --keychain-profile agent-pet
```

An empty history that returns cleanly is a pass.

Storing credentials keeps secrets off the command line, where `ps` would show
them to any process on the machine, and out of shell history.

### 4. Name the profile for this checkout

```bash
echo agent-pet > .notary-profile
```

That is the whole of it — `make notarize` takes the name from there and needs
no arguments after this. The file is git-ignored, and has to be: it names a
keychain item that exists only on the machine that ran `store-credentials`, so
a committed copy would point everyone else at a profile they do not have. It is
not itself a secret; the credentials never leave the keychain.

---

## Cutting a release

Tag first. The tag is the version: it reaches the binary through `-ldflags` and
`Info.plist` through `scripts/brand.sh`, and an untagged tree builds as whatever
`wails.json` says.

```bash
git tag v0.1.0            # the release app
git tag v0.1.0-dev.1      # the dev app, from the same commit
```

```bash
make release
```

That runs the build, this script, the upload, and the manifest write as one
command — and refuses before signing anything if the tag and the built bundle do
not name the same version and the same app. `make release CHANNEL=dev` publishes
the dev app, taking the prerelease tag on the same commit.

The two steps underneath it are still there when you want to sign without
publishing:

```bash
make build
```

```bash
make notarize
```

Either way you get `build/bin/agent-pet-<version>-universal.zip`, stapled.
Submission to Apple usually takes a few minutes. `NOTARY_PROFILE=other-name`
overrides `.notary-profile` for one run.

**Signing it is not offering it.** `make release` leaves
`updates/<channel>.json` modified and uncommitted; committing that file is what
points every installed pet at the new version. See
[ADR 0008](adr/0008-over-the-air-updates.md).

Neither Xcode nor a paid toolchain is needed beyond this: `notarytool` and
`stapler` ship with the Command Line Tools.

**This happens on your machine, not on a runner.** There is no signing job in
CI and that is deliberate: a runner can only sign if the certificate's private
key is copied into repository secrets, and that key is the one thing that
cannot be re-issued freely — Apple caps how many Developer ID certificates an
account may hold. Releases here are cut by hand, from the one machine that
holds it.

---

## What the script does, and why each step matters

Each of these is a way to lose an afternoon.

**Signs inside out.** Nested binaries first (`petctl`), then the bundle.
Signing the bundle seals what is nested inside it, so anything signed afterwards
invalidates the seal. Apple's `--deep` shortcut for this is deprecated and
applies the outer options to nested code.

**`--options runtime`.** The hardened runtime. Notarization rejects anything
without it. This app needs **no entitlements** — WebKit, the cgo status item and
the loopback server all start under it, which was tested rather than assumed. If
that ever changes, the symptom is the app failing to launch after signing while
the unsigned build is fine.

**`--timestamp`.** A secure timestamp is what keeps a signature valid after the
certificate expires. Without it, everything you shipped stops validating the day
the certificate lapses.

**`ditto`, never `zip`.** A bundle carries symlinks and extended attributes that
`zip` silently drops. The result fails to launch in a way that looks exactly
like a signing fault.

**Repackages after stapling.** Stapling attaches the notarization ticket to the
bundle so it launches offline. The zip made before stapling holds a copy
without the ticket, so it is rebuilt afterwards.

**Verifies the zip, not the bundle it stapled.** The last checks unpack the
finished archive into a temporary directory and run `codesign`, `spctl` and
`stapler validate` on what comes out. The archive is rebuilt after stapling, so
it is not the file any earlier step looked at — and since `petctl update`
installs it unattended, "the directory on this disk is fine" is not the question
worth answering.

**Checks for the profile before it signs anything.** A missing profile found
after the bundle is signed and zipped costs a minute to learn something it
could have said at once.

**Discovers the bundle rather than naming it.** The app has been renamed once
(`digital-pet` → `agent-pet`). A hardcoded path survives a rename looking
correct while signing nothing.

---

## Verifying

```bash
codesign --verify --strict --verbose=2 build/bin/agent-pet.app
```

```bash
spctl -a -t exec -vv build/bin/agent-pet.app
```

`spctl` should say `source=Notarized Developer ID`. That is the check that
matters: it is Gatekeeper's own answer, and it is what a user's machine will
decide.

```bash
xcrun stapler validate build/bin/agent-pet.app
```

The install skill in the plugin runs the first two before it will move anything
into `/Applications`, and stops rather than working around a failure.

---

## When it goes wrong

| Symptom | Cause |
|---|---|
| `no notarytool profile` | `.notary-profile` is missing — step 4 above |
| `no 'Developer ID Application' certificate` | Not created yet, or the wrong type was created |
| Identity listed but `codesign` fails | Intermediate missing from the chain — [apple.com/certificateauthority](https://www.apple.com/certificateauthority/) |
| Certificate present, no private key under it | The CSR was generated on a different machine |
| `The data couldn't be read…` in Xcode | Enrolment still pending, or the licence agreement is unaccepted |
| Notarization rejected | Ask Apple; the log names the binary and the reason |
| App launches unsigned, fails signed | Hardened runtime needs an entitlement |
| Update appears to do nothing | An old `petd` still holds the port; the new one exits quietly. `petctl update` polls the port for this reason rather than trusting the quit |
| `petctl update --check` says nothing is published | True until `updates/<channel>.json` is committed — signing a release does not offer it |

```bash
xcrun notarytool log <submission-id> --keychain-profile agent-pet
```

Get the submission id from `xcrun notarytool history --keychain-profile agent-pet`.

