# Offline Agent package signing (development)

Build the offline utility from this development branch: go build -o agent-package ./cmd/agent-package

Use a trusted, isolated signing workstation. The utility does not contact a network.
Generate the production key on that workstation, not in CI or on Dashboard.
For example, with OpenSSL:

    umask 077
    openssl genpkey -algorithm ED25519 -out release.pem
    openssl pkey -in release.pem -pubout -out release.pub.pem

Keep release.pem offline, encrypted at rest with access control, and backed up securely.
Never send it to Dashboard, Agent, GitHub or a chat. On Windows, restrict ACLs manually.
The tool accepts an unencrypted PKCS#8 PEM key for the duration of the signing operation.
Public key provisioning and rotation are not implemented yet.

After reviewing the built Agent artifact and its platform and version, create a new package:

    ./agent-package -binary ./nezha-agent -out ./agent-linux-amd64.nzu -key ./release.pem -key-id release-01 -os linux -arch amd64 -version v0.30.01 -min-version v0.30.00 -sequence 2

The tool calculates binary SHA256, signs the manifest, builds the bounded ZIP,
then independently verifies the package before publishing a new output path.
The output filesystem must support hard links. Existing output files are never replaced.
The package signature authenticates the exact bytes and operator-supplied metadata;
the tool does not prove that a binary is the expected Agent build or that its platform
metadata is correct. Production signing requires CI artifact provenance and review.
Never reuse a release sequence for a different release; sequence assignment and key
rotation require an operational release register. Same-version packages for different
platforms can use the same release sequence.

This is a development tool, not a runtime upgrade or deployment endpoint.
