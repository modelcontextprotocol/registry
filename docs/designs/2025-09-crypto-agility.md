# Cryptographic agility for MCP Registry authentication

- Author: Joel Verhagen
- Date: September 24, 2025
- Issue: https://github.com/modelcontextprotocol/registry/issues/482

## Introduction

Today, the Community MCP Registry supports the following authentication methods for publishing MCP Servers:

- GitHub (OIDC or interactive)
- HTTP
- DNS

See the [publish a server](https://github.com/modelcontextprotocol/registry/blob/747ad310bba75d80f20c1dd03051dd825df734e0/docs/guides/publishing/publish-server.md) guide for more information on the current authentication flows.

GitHub authentication only unlocks the `io.github.<username>/*` namespaces. It does not support custom namespaces based on DNS names.

For this document, we will focus on the HTTP and DNS authentication mechanisms because they leverage cryptographic key pairs (where crypto agility is needed) and enable custom namespaces based on domain name.

Today, the only cryptographic algorithm supported is `ed25519`. The idea of this document is to suggest a way new cryptographic algorithms can be introduced without disrupting the general protocol or existing integrations.

## Goals

The goals of this document are to:

- Formalize the structure of the DNS and HTTP public key "proof record"
- Propose an additional crypto algorithm that is supported by HSM-backed cloud-based key services
- Describe at a high level the code changes needed in the MCP Registry code repository

The purpose of this work is to improve the authentication flow's compliance with Microsoft security and compliance guidelines. This will allow Microsoft teams to authenticate to a Microsoft namespace and publish their MCP servers.

## Non-goals

The non-goals (things that will not be discussed or decided upon) are:

- Describe how specific cloud-based key services would integrate into client tooling
- Change the authentication flow to add any new parameters or steps
- Critique or harden the authentication flow, such as by incorporating additional payload into the login flow or limiting the duration for which a specific key pair can be used

## Authentication overview

The HTTP and DNS authentication mechanisms are conceptually the same. The owner of a namespace such as `com.joelverhagen/*` proves ownership by posting a public key in a place only a controller of that namespace can modify. For DNS, this means putting a TXT record on "joelverhagen.com". For HTTP, this means serving an HTTP payload at `/.well-known/mcp-registry-auth`.

In other words, if you can update the DNS records or the web service hosted at a domain name, you can use the namespace for your MCP servers.

Aside from declaring the public key as mentioned above, the private key is used to sign a timestamp. The signature, the original timestamp, and the namespace (domain) are sent to a `/v0/auth/dns` or `/v0/auth/http` registry endpoint.

The registry then fetches public keys from the domain using the DNS- or HTTP-specific method. DNS can have multiple keys via multiple TXT records. HTTP only supports a single public key. If one of the public keys matches the signature provided in the payload, and the timestamp is within an acceptable window, the request is accepted and a JWT minted by the registry is returned.

The JWT contains standard claims such as expiry and also contains permissions indicating which namespaces are allowed.

At this point, the login flow is complete and the JWT can be used for operations such as publishing an MCP server.

## Choice of algorithm

The format of the DNS and HTTP public key declaration is:

`v=MCPv1; k=ed25519; p=<public key bytes>`

Conceptually, it is a set of key-value pairs. Today the only variable part in the string is the public key bytes.

I propose the following backward-compatible enhancements:

- Allow the key `k=` ("key pair algorithm name") to support values besides `ed25519`.
  - The algorithm name should encompass all parameters needed to perform crypto sign and verify operations aside from the public key and the private key. In other words, the algorithm name might include both a crypto system name and specific parameters like a name of a well-known ECC curve (for example). It need not be an industry-recognized string but must be a well-defined value agreed upon by the MCP client tool and MCP Registry.
- Allow the key `p=` to contain a public key string that is processed using a routine defined by the `k=` value.
  - The only requirement on the string is that it does not contain a semicolon `;` because this is the delimiter for the key-value pairs.

One concern for the `k=` value is how long it can get. A DNS TXT record has a maximum length of 255 characters. In other words, a public key that is too long (such as a large RSA public key) may not fit. The DNS flow lends itself to elliptic curve cryptography, which generally has shorter public keys than RSA-based cryptosystems. The HTTP flow can facilitate longer public key representations.

Future major versions of the record format can be differentiated by `v=MCPv2` or similar.

As the need arises for additional crypto algorithms, values allowed by the `k=` parameter can be expanded ("crypto agility") and older ones can be phased out by the MCP Registry, as needed. 

## Supported algorithms

The values supported by the `k=` parameter would be defined by the MCP Registry deployment and what support exists in the associated client publish tool. 

For the official MCP Registry, I propose the following crypto algorithms.

### Ed25519

This is the existing support for backward compatibility and is the default.

The `k=ed25519` value refers to the [Ed25519](https://en.wikipedia.org/wiki/EdDSA#Ed25519) algorithm.

The `p=` value is the base64 encoding of the public key bytes. The public key is 32 bytes prior to base64 encoding.

The signature sent during the authentication step uses the local Ed25519 private key to sign the current timestamp, represented in RFC 3339 UTC format. Both the timestamp and the hex-encoded signature bytes are included in the authentication request.

An example DNS record looks like this:
```
v=MCPv1; k=ed25519; p=OHjrTGdvR2dFk1g5uTVNJ4/RxpDLYjVJTtTQlcwW0Jg=
```

### ECDSA, NIST P-384 curve

This is a newly supported crypto algorithm proposed by this document. It is the ECDSA cryptosystem using a specific NIST-approved curve: P-384. The parameters for this curve are [well documented](https://nvlpubs.nist.gov/nistpubs/FIPS/NIST.FIPS.186-4.pdf). This curve is supported by the Go standard library (which the MCP publish tool and Registry are implemented in) and by cloud-based secret stores such as Azure Key Vault and AWS KMS (both of which support storing keys in HSMs).

The algorithm will be represented as `k=ecdsap384`.

The `p=` value will be the public key, compressed using the format described in SEC 1, Version 2.0, Section 2.3.3, and base64-encoded. The compressed public key is 49 bytes prior to base64 encoding. The compressed format is used to reduce the number of characters in the DNS TXT record.

The public key will be decompressed by the MCP Registry service using the [`UnmarshalCompressed` function](https://pkg.go.dev/crypto/elliptic#UnmarshalCompressed) in the `crypto/elliptic` standard library package.

The timestamp string will be hashed using the SHA2-384 hashing algorithm. This hash will be signed using the P-384 private key either in-memory in the publish tool or via a cloud-based key signing service.

The signature will be represented in the `R || S` format (the concatenation of the output R and S values) and will be hex-encoded, similar to the Ed25519 flow. The signature is 96 bytes prior to hex encoding.

The service will hash the timestamp in the same way as the client, extract the R and S values from the signature, and verify them against the public keys found via DNS or HTTP-based public key discovery as mentioned above. 

#### Generating a key pair

The MCP Server author can generate a new ECDSA P-384 key pair with this OpenSSL command:

```
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:secp384r1 -out <pem path>
```

#### Getting the public key string (for the DNS record)

The MCP Server author can get the compressed, encoded public key value from the `.pem` using the following OpenSSL command:

```
openssl ec -in <pem path> -text -noout -conv_form compressed | grep -A4 "pub:" | tail -n +2 | tr -d ' :\n' | xxd -r -p | base64
```

This value will be placed in the HTTP response payload (using a web service of their choice) or in the DNS TXT record (using whatever tool or web app is applicable to their DNS provider).

#### Getting the private key string (for publishing)

The MCP Server author can get the encoded private key value to provide to the publish tool using this OpenSSL command:
```
openssl ec -in <pem path> -noout -text | grep -A4 "priv:" | tail -n +2 | tr -d ' :\n'
```

#### Log in to the registry

The private key can be used to log in to the MCP Registry (perform the described authentication flow) using the following command:

```
./publisher login dns -algorithm ecdsap384 -domain "<domain name>" -private-key "<hex private key>"
```

#### Example DNS record

```
v=MCPv1; k=ecdsap384; p=A2hCpZoIur1vFajkiVi3s7PVhaEpgLyg8PaIEt2Z6oqFDTG2BqF+7bBcZG7pExpkgw==
```

The equivalent public key in uncompressed hex form is:

```
pub:
    04:68:42:a5:9a:08:ba:bd:6f:15:a8:e4:89:58:b7:
    b3:b3:d5:85:a1:29:80:bc:a0:f0:f6:88:12:dd:99:
    ea:8a:85:0d:31:b6:06:a1:7e:ed:b0:5c:64:6e:e9:
    13:1a:64:83:3f:9e:fa:33:40:d3:b5:39:e8:fb:f7:
    22:32:14:6a:c9:98:63:db:bb:a0:ed:fb:22:e4:48:
    7b:e2:c4:bd:f7:54:23:0d:d9:f5:63:2e:cd:b7:0a:
    98:58:16:3a:90:27:b3
```

## The future and new algorithms

I believe we can anticipate two categories of changes needed for the MCP Registry authentication flow in the future.

**We may need incremental changes related to new crypto algorithms.**

The currently supported key pair algorithms will age out due to advances in computing power, advances in quantum computing, or vulnerabilities found in existing algorithms being relied upon. Alternatively, some enterprise environments may have certain crypto algorithms banned or blessed, necessitating that a new algorithm be added to unblock the enterprise from publishing to the registry in a compliant manner (as is the case for Microsoft). In situations like this, a new key pair algorithm will be proposed as a safe alternative.

In these cases, we can simply introduce a new `k=` algorithm name, update the related publisher tool and registry service code, and plan the phase-out of the older algorithm (if needed).

I recommend that these proposals have a low bar and can be driven by any community member with a GitHub issue and accompanying pull request. Factors to consider during design and implementation include:

- Is the algorithm supported by the Go standard library? This lowers the bar of implementation since no new dependency is needed.
- Is the algorithm just a new curve or a new algorithm entirely? New curves can be implemented by copying or sharing code with an existing `k=` value.

New algorithms should not be the opportunity to introduce additional inputs for the sign or verification steps.

A simple checklist to add a new `k=` algorithm could be:
- [ ] Add support for the `k=` value to the publisher tool
  - New value for the `-algorithm` command line argument
  - New switch case for parsing the private key argument
  - New switch case for signing with the private key
- [ ] Add support for the `k=` value to the registry service   
  - New switch case for parsing the public key from the proof record
  - New switch case for executing the verify step (and timestamp hash, if applicable)
- [ ] Mention the newly supported algorithm in the publishing docs
  - Document the OpenSSL command for generating the key pair
  - Document the OpenSSL command for producing the `p=` public key base64 string for the proof record
  - Document the OpenSSL command for producing the private key hex string for the login command

In short, these types of changes are backward compatible and pose little burden on the MCP Registry maintainers.

**We may need fundamental changes related to the MCP Registry authentication flow.**

If vulnerabilities are found in the current flow, or a new required step is needed, then we could consider the changes a breaking protocol change. Some changes may be accommodated by incrementing the `v=` parameter of the DNS TXT record or HTTP payload to, say, `v=MCPv2` and changing the rest of the record's key-value pairs. Or, the changes may be outside of the proof record entirely and may mean a change to the token exchange request. This could be facilitated with an API version parameter, such as changing the URL to `/v1/auth/dns`, adding an API version to an `Accept` or `Content-Type` request header, a query parameter, or a field in the JSON request body.

These changes are out of scope for this document since I am focusing on crypto agility in the places we currently use crypto algorithms.

If a pressing security issue is found in the MCP Registry, the [Security Policy](https://github.com/modelcontextprotocol/registry/security) should be used to ensure responsible disclosure and remediation.

## Code changes

Two areas need to be updated to support this new flow.

### Publisher tool changes

A new `-algorithm` parameter must be added to the `login` command. It only applies to the `dns` and `http` methods.

The allowed values for this new parameter will match the supported `k=` values, i.e. `ed25519` and `ecdsap384`.

The `-algorithm` parameter will be optional and default to `ed25519` for backward compatibility.

The authentication code will parse the provided private key bytes using the [`ParseRawPrivateKey`](https://pkg.go.dev/crypto/ecdsa#ParseRawPrivateKey) function in the `crypto/ecdsa` standard library package.

The SHA2-384 hash operation and the ECDSA sign operation will also both be performed using the Go standard library.

The main routine that will be modified is `GetToken` in [`/registry/cmd/publisher/auth/common.go](https://github.com/modelcontextprotocol/registry/blob/main/cmd/publisher/auth/common.go#L24-L59)

### Registry service changes

No new endpoints are needed. Instead, the `/v0/auth/http` and `/v0/auth/dns` endpoints will be modified to support the flow in a backward-compatible manner.

The string found in the DNS TXT records and HTTP response will now be parsed to allow a flexible `k=` value. The service will reject any value that is not `ed25519` or `ecdsap384`.

The `p=` value will be parsed based on the `k=` value, and an appropriate public key structure will be created.

The `k=` value will also be used to process the provided signature value. The existing processing of the signature will be gated on an `ed25519` case. The new `ecdsap384` case will perform the needed SHA2-384 operation and cryptographic verify operation using the public key found in the DNS TXT record or HTTP response.

The MCP Server author will still be able to use multiple DNS TXT records and can even use `ed25519` and `ecdsap384` side by side.

The main routine that will be modified is `ExchangeToken` in [`/internal/api/handlers/v0/auth/dns.go`](https://github.com/modelcontextprotocol/registry/blob/747ad310bba75d80f20c1dd03051dd825df734e0/internal/api/handlers/v0/auth/dns.go#L89-L161) and in [`/internal/api/handlers/v0/auth/http.go`](https://github.com/modelcontextprotocol/registry/blob/747ad310bba75d80f20c1dd03051dd825df734e0/internal/api/handlers/v0/auth/http.go#L133-L196). These two modules will be refactored to share more code. There is some unnecessary duplication right now.
