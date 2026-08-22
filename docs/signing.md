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
password. Without the private key the certificate is inert, only a small number
of Developer ID certificates may exist per account, and this same file becomes
the CI secret later.

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
them to any process on the machine, and out of shell history and CI logs.

---

## Cutting a release

```bash
make build
```

```bash
make notarize NOTARY_PROFILE=agent-pet
```

That leaves `build/bin/agent-pet-<version>-universal.zip`, stapled and ready to
attach to a GitHub release. Submission to Apple usually takes a few minutes.

Neither Xcode nor a paid toolchain is needed beyond this: `notarytool` and
`stapler` ship with the Command Line Tools.

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
| `no 'Developer ID Application' certificate` | Not created yet, or the wrong type was created |
| Identity listed but `codesign` fails | Intermediate missing from the chain — [apple.com/certificateauthority](https://www.apple.com/certificateauthority/) |
| Certificate present, no private key under it | The CSR was generated on a different machine |
| `The data couldn't be read…` in Xcode | Enrolment still pending, or the licence agreement is unaccepted |
| Notarization rejected | Ask Apple; the log names the binary and the reason |
| App launches unsigned, fails signed | Hardened runtime needs an entitlement |
| Update appears to do nothing | An old `petd` still holds the port; the new one exits quietly |

```bash
xcrun notarytool log <submission-id> --keychain-profile agent-pet
```

Get the submission id from `xcrun notarytool history --keychain-profile agent-pet`.

---

## In CI

`scripts/notarize.sh` takes credentials from the environment when `--profile`
is absent, so the local and CI paths run the same code:

| Secret | From |
|---|---|
| `MACOS_CERT_P12` | `base64 -i cert.p12` |
| `MACOS_CERT_PASSWORD` | the export password |
| `NOTARY_ISSUER_ID` | App Store Connect (team keys only) |
| `NOTARY_KEY_ID` | App Store Connect |
| `NOTARY_KEY_P8` | the `.p8`, base64 |

The runner imports the `.p12` into a temporary keychain it deletes afterwards.
The API key is written to a temporary file rather than passed as an argument,
so it stays out of the process list and the build log.
