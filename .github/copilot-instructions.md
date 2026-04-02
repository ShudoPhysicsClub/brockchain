# Copilot Instructions for Brockchain

## Project Overview

**Brockchain** is a Proof-of-Work (PoW) blockchain implementation being rewritten from Node.js to Go.

- **Consensus**: PoW (no smart contracts)
- **Language**: Go 1.26.1
- **Organization**: ShudoPhysicsClub
- **Module Path**: `github.com/ShudoPhysicsClub/brockchain`

## Architecture

### Core Modules (`server/module/`)

1. **chain/** - Blockchain and block management
   - Block validation and storage
   - Chain consensus rules

2. **crypto/** - Cryptographic operations
   - ECDSA P256 elliptic curve signatures
   - Key generation and verification

3. **mempool/** - Transaction mempool
   - Transaction queue management
   - Transaction validation

4. **network/** - P2P networking
   - Node communication
   - Block and transaction propagation

## Development Conventions

### Go Standards

- Follow [Effective Go](https://go.dev/doc/effective_go) style guide
- Use `gofmt` for formatting (enforced by Go)
- Package names lowercase, single word preferred
- Interface names: `Reader`, `Writer`, `Handler` (verb-er pattern)
- Exported functions/types start with uppercase; unexported start with lowercase

### Code Organization

```
server/
├── main.go           # Entry point and CLI
├── go.mod           # Module definition
└── module/
    ├── chain/       # Blockchain logic
    ├── crypto/      # ECDSA implementation
    ├── mempool/     # Transaction pool
    └── network/     # P2P communication
```

### Module Boundaries

- **crypto** is isolated and has no dependencies on other modules
- **mempool** integrates transaction validation from crypto
- **chain** coordinates block creation, validation, and PoW
- **network** distributes blocks and transactions
- **main** orchestrates initialization and CLI

## Build & Development

### Build the Project

```bash
cd server
go build -o brockchain .
```

### Run Tests

```bash
cd server
go test ./...
```

### Development Tips

- Use `go mod tidy` to clean up dependencies
- Use `go fmt ./...` to format code
- Use `go vet ./...` to catch common issues
- Enable Go errors view in VS Code for real-time linting

## Key Implementation Details

### Cryptography (crypto/ecsh.go)

- **Curve**: P256 ECDSA
- **Key Types**:
  - `PrivateKey`: [32]byte
  - `PublicKey`: [64]byte (X:32 + Y:32)
  - `Signature`: [96]byte (Rx:32 + Ry:32 + S:32)
- **Utilities**:
  - `bigToBytes32()` - Convert big.Int to 32-byte array
  - `bytesToBig()` - Convert bytes to big.Int

### Proof of Work

- **Algorithm**: (To be documented)
- **Difficulty**: (To be documented)
- **Target Block Time**: (To be documented)

## Common Tasks

### Adding a New Function to a Module

1. Define in the appropriate module package
2. Export (uppercase first letter) or keep private (lowercase)
3. Add docstring following Go conventions: `// FunctionName does X.`
4. Write tests in a `*_test.go` file

### Modifying Crypto Functions

- Changes impact chain validation, so coordinate across modules
- Add tests to verify backward compatibility considerations

### Testing

- Use Go's built-in `testing` package
- Run `go test ./module/...` to test specific modules
- Aim for high coverage in crypto/chain modules

## Resources

- **Go Documentation**: https://go.dev/doc/
- **ECDSA**: https://en.wikipedia.org/wiki/Elliptic_Curve_Digital_Signature_Algorithm
- **Blockchain**: Study existing PoW implementations (Bitcoin, Ethereum 1.0)

## Next Steps (Suggested)

- [ ] Implement chain/Block type and validation
- [ ] Implement chain/BlockHeader type
- [ ] Implement mempool/Transaction type
- [ ] Implement PoW difficulty calculation
- [ ] Implement network P2P protocol
- [ ] Create main CLI interface

## Notes

- This is an early-stage project; architecture may evolve
- Smart contracts intentionally excluded for simplicity
- Japanese comments welcome in code for team clarity
