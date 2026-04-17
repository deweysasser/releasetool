# golang-program

A template repository for creating Go CLI utility programs with batteries included.

## Overview

This is a template project that provides all the boilerplate needed to quickly build a Go CLI utility. 
Instead of setting up CLI parsing, logging, build systems, and Docker configurations from scratch, 
you can clone this template and immediately start writing your actual program logic.

## Features

- **CLI Argument Parsing** - Using [Kong](https://github.com/alecthomas/kong) with struct tags for clean, declarative CLI definitions
- **Structured Logging** - [Zerolog](https://github.com/rs/zerolog) with automatic format detection (human-readable terminal output or JSON)
- **Version Management** - Automatic version embedding from git tags via linker flags
- **Build System** - Makefile with targets for building, testing, packaging, and Docker images
- **Cross-Platform** - Windows and Unix support with proper color terminal handling
- **Docker Support** - Multi-stage builds using distroless base images
- **Testing Setup** - Example tests with testify, output capturing, and function mocking
- **Logging Levels** - Debug, Info, and Quiet modes built-in
- **Context Integration** - Logger and options available throughout the application via dependency injection

## Getting Started

### Using This Template

1. **Clone or fork this repository**
   ```bash
   git clone https://github.com/deweysasser/golang-program your-project-name
   cd your-project-name
   ```

2. **Update the module and import paths** to match your new repository
   ```bash
   make update-repo NEWREPO=github.com/yourusername/your-project-name
   ```
   Or, if you've already set the git origin remote, it will auto-detect:
   ```bash
   make update-repo
   ```
   This updates `go.mod`, `main.go`, `.chglog/config.yml`, and any other `.go` files that reference the template module path.

3. **Build and run**
   ```bash
   make
   ./your-project-name --help
   ```

### Adding Your Program Logic

The main program logic goes in `program/program.go`:

```go
func (program *Options) Run() error {
    // Your program logic here
    log.Info().Msg("Hello from your program!")
    return nil
}
```

### Adding CLI Options

Add fields to the `Options` struct in `program/program.go`:

```go
type Options struct {
    Version bool `help:"Show program version"`

    Debug        bool   `group:"Info" help:"Show debugging information"`
    OutputFormat string `group:"Info" enum:"auto,jsonl,terminal" default:"auto" help:"How to show program output"`
    Quiet        bool   `group:"Info" help:"Be less verbose than usual"`

    // Add your options here:
    InputFile  string   `arg:"" help:"Input file to process"`
    Timeout    int      `help:"Timeout in seconds" default:"30"`
    Verbose    bool     `short:"v" help:"Verbose output"`
}
```

### Using Dependency Injection

Any function you call from `Run()` can receive bound types as arguments:

```go
func (program *Options) Run(ctx context.Context, logger zerolog.Logger) error {
    logger.Info().Msg("Logger is automatically injected")

    // Call other functions with injected dependencies
    return doWork(ctx, logger, program)
}

func doWork(ctx context.Context, logger zerolog.Logger, opts *Options) error {
    logger.Debug().Str("input", opts.InputFile).Msg("Processing file")
    // Your logic here
    return nil
}
```

### Keeping up to date with this template

You can use this template projects as "inheritance" by adding it as a remote and pulling changes.  
Git is quite good at merging, and this even allows you to make changes to files defined in the template
without worrying about your changes being lost.

```bash
git remote add template https://github.com/deweysasser/golang-program
git fetch template
git pull template main
```

## Building

### Local Build
```bash
make                    # Build the binary
make install            # Install to $GOPATH/bin
make test               # Run tests
make vet                # Run go vet
```

### Docker Build
```bash
make image              # Build Docker image
docker run your-project-name --help
```

### Versioning

Version is automatically determined from git:
```bash
make VERSION=v1.2.3     # Override version
make                    # Uses git describe --tags --always --dirty
```

## Testing

Tests are in `program/program_test.go`. Run with:
```bash
make test
go test -v ./...                    # All tests with verbose output
go test -v ./... -run TestName      # Single test
```

## CI/CD and Releases

This project includes three GitHub Actions workflows:

### Build (`.github/workflows/build.yaml`)

Runs on every push to any branch:
- Runs `go vet`, tests, and builds the binary
- Builds and pushes a Docker image to GHCR tagged with the branch name
- Checks code formatting with `go fmt`

### Test on All Platforms (`.github/workflows/test-multiplatform.yaml`)

Runs on pull requests and manual dispatch:
- Runs tests on Linux, macOS, and Windows

### Release (`.github/workflows/release.yml`)

Triggered by pushing a `v*` tag:
- Builds cross-platform binaries (linux/darwin/windows x amd64/arm64) and packages them as zip files
- Builds and pushes a Docker image to GHCR tagged with the version and `latest`
- Generates a changelog and creates a GitHub Release with the zip artifacts

### Creating a Release

Tag a commit with a version and push the tag:
```bash
git tag v1.0.0
git push origin v1.0.0
```

The release workflow handles everything automatically — building binaries, creating the Docker image, and publishing the GitHub Release.

## Logging

The program automatically detects terminal output and formats logs accordingly:

**Terminal output (human-readable):**
```
2:04PM INF Starting version=v1.0.0 program=./myapp
```

**JSON output (when piped or `--output-format=jsonl`):**
```json
{"level":"info","time":"2025-11-11T14:04:00Z","message":"Starting","version":"v1.0.0"}
```

Control logging levels:
- `--debug` - Show debug messages
- `--quiet` - Only show warnings and errors
- Default - Show info level and above

## Project Structure

```
.
├── main.go                    # Entry point, sets up Kong and context
├── program/
│   ├── program.go            # Options struct and Run() implementation
│   ├── program_test.go       # Tests
│   └── version.go            # Version variable (set at build time)
├── Makefile                   # Build targets
├── Dockerfile                 # Multi-stage Docker build
└── go.mod                     # Dependencies
```

## Dependencies

- [Kong](https://github.com/alecthomas/kong) - Command-line parser with struct tags
- [Zerolog](https://github.com/rs/zerolog) - Fast, structured logging
- [Testify](https://github.com/stretchr/testify) - Testing assertions
- [go-colorable](https://github.com/mattn/go-colorable) - Cross-platform colored output

## License

Update with your license information.
