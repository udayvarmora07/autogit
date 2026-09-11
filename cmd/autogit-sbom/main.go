// Command autogit-sbom turns the Go module graph into a dependency SBOM.
//
// The command intentionally records module identities rather than local
// filesystem paths. This makes the release document safe to publish and
// deterministic when its creation time is supplied by the release workflow.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	spdxVersion = "SPDX-2.3"
	dataLicense = "CC0-1.0"
)

type module struct {
	Path    string  `json:"Path"`
	Version string  `json:"Version"`
	Main    bool    `json:"Main"`
	Replace *module `json:"Replace"`
}

type options struct {
	Name       string
	Namespace  string
	RootModule string
	Version    string
	Format     string
	Created    time.Time
}

type document struct {
	SPDXVersion       string         `json:"spdxVersion"`
	DataLicense       string         `json:"dataLicense"`
	SPDXID            string         `json:"SPDXID"`
	Name              string         `json:"name"`
	DocumentNamespace string         `json:"documentNamespace"`
	CreationInfo      creationInfo   `json:"creationInfo"`
	Packages          []spdxPackage  `json:"packages"`
	Relationships     []relationship `json:"relationships"`
}

type creationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	SPDXID           string `json:"SPDXID"`
	Name             string `json:"name"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
}

type relationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

type cyclonedxDocument struct {
	BOMFormat    string                `json:"bomFormat"`
	SpecVersion  string                `json:"specVersion"`
	SerialNumber string                `json:"serialNumber"`
	Version      int                   `json:"version"`
	Metadata     cyclonedxMetadata     `json:"metadata"`
	Components   []cyclonedxComponent  `json:"components"`
	Dependencies []cyclonedxDependency `json:"dependencies"`
}

type cyclonedxMetadata struct {
	Timestamp string             `json:"timestamp"`
	Tools     []cyclonedxTool    `json:"tools"`
	Component cyclonedxComponent `json:"component"`
}

type cyclonedxTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type cyclonedxComponent struct {
	Type    string `json:"type"`
	BomRef  string `json:"bom-ref"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Scope   string `json:"scope,omitempty"`
}

type cyclonedxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

func main() {
	output := flag.String("output", "", "write the SBOM JSON document to this path")
	format := flag.String("format", "spdx", "SBOM format: spdx or cyclonedx")
	name := flag.String("name", "autogit", "SBOM document name")
	namespace := flag.String("namespace", "", "globally unique SBOM document namespace")
	rootModule := flag.String("root-module", "", "main Go module path; inferred when omitted")
	version := flag.String("version", "NOASSERTION", "release version for the main module")
	created := flag.String("created", "", "creation time in RFC3339 format")
	flag.Parse()

	if err := run(os.Stdin, os.Stdout, *output, options{
		Name:       *name,
		Namespace:  *namespace,
		RootModule: *rootModule,
		Version:    *version,
		Format:     *format,
		Created:    parseCreated(*created),
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseCreated(value string) time.Time {
	if value == "" {
		return time.Now().UTC()
	}
	created, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return created.UTC()
}

func run(input io.Reader, stdout io.Writer, output string, opts options) error {
	if opts.Name == "" || opts.Namespace == "" || opts.RootModule == "" || opts.Version == "" || opts.Created.IsZero() {
		return errors.New("name, namespace, root-module, version, and valid created time are required")
	}
	modules, err := decodeModules(input)
	if err != nil {
		return err
	}
	format := strings.ToLower(opts.Format)
	if format == "" {
		format = "spdx"
	}
	var document any
	switch format {
	case "spdx":
		document, err = buildDocument(modules, opts)
	case "cyclonedx", "cyclone-dx":
		document, err = buildCycloneDocument(modules, opts)
	default:
		return fmt.Errorf("unsupported SBOM format %q", opts.Format)
	}
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s document: %w", format, err)
	}
	encoded = append(encoded, '\n')
	if output == "" {
		_, err = stdout.Write(encoded)
		return err
	}
	if err := os.WriteFile(output, encoded, 0600); err != nil {
		return fmt.Errorf("write %s document: %w", format, err)
	}
	return nil
}

func decodeModules(input io.Reader) ([]module, error) {
	decoder := json.NewDecoder(input)
	var modules []module
	for {
		var item module
		err := decoder.Decode(&item)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode Go module graph: %w", err)
		}
		if strings.TrimSpace(item.Path) == "" {
			return nil, errors.New("decode Go module graph: module path is empty")
		}
		modules = append(modules, effectiveModule(item))
	}
	if len(modules) == 0 {
		return nil, errors.New("decode Go module graph: no modules")
	}
	return modules, nil
}

func effectiveModule(item module) module {
	if item.Replace == nil || item.Replace.Path == "" {
		return item
	}
	// A local replacement's Path is a filesystem path in the go list output.
	// Preserve the requested module identity instead of publishing that path.
	if strings.HasPrefix(item.Replace.Path, "/") || strings.HasPrefix(item.Replace.Path, "./") || strings.HasPrefix(item.Replace.Path, "../") {
		item.Version = item.Replace.Version
		item.Replace = nil
		return item
	}
	replacement := *item.Replace
	replacement.Main = item.Main
	replacement.Replace = nil
	return replacement
}

func buildDocument(modules []module, opts options) (document, error) {
	rootIndex := -1
	for i := range modules {
		if modules[i].Main && modules[i].Path == opts.RootModule {
			rootIndex = i
			break
		}
	}
	if rootIndex < 0 {
		return document{}, fmt.Errorf("main module %q was not found in the Go module graph", opts.RootModule)
	}

	unique := make(map[string]module, len(modules))
	for _, item := range modules {
		version := item.Version
		if item.Main {
			version = opts.Version
		}
		key := item.Path + "\x00" + version
		item.Version = version
		unique[key] = item
	}
	items := make([]module, 0, len(unique))
	for _, item := range unique {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Version < items[j].Version
		}
		return items[i].Path < items[j].Path
	})

	root := module{Path: opts.RootModule, Version: opts.Version, Main: true}
	rootID := packageID(root)
	packages := make([]spdxPackage, 0, len(items))
	relationships := []relationship{{
		SPDXElementID:      "SPDXRef-DOCUMENT",
		RelationshipType:   "DESCRIBES",
		RelatedSPDXElement: rootID,
	}}
	for _, item := range items {
		id := packageID(item)
		license := "NOASSERTION"
		if item.Main {
			license = "Apache-2.0"
		}
		version := item.Version
		if version == "" {
			version = "NOASSERTION"
		}
		packages = append(packages, spdxPackage{
			SPDXID:           id,
			Name:             item.Path,
			VersionInfo:      version,
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    false,
			LicenseConcluded: "NOASSERTION",
			LicenseDeclared:  license,
			CopyrightText:    "NOASSERTION",
		})
		if id != rootID {
			relationships = append(relationships, relationship{
				SPDXElementID:      rootID,
				RelationshipType:   "DEPENDS_ON",
				RelatedSPDXElement: id,
			})
		}
	}

	return document{
		SPDXVersion:       spdxVersion,
		DataLicense:       dataLicense,
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              opts.Name,
		DocumentNamespace: opts.Namespace,
		CreationInfo: creationInfo{
			Created:  opts.Created.UTC().Format(time.RFC3339),
			Creators: []string{"Tool: autogit-sbom"},
		},
		Packages:      packages,
		Relationships: relationships,
	}, nil
}

func buildCycloneDocument(modules []module, opts options) (cyclonedxDocument, error) {
	rootIndex := -1
	for i := range modules {
		if modules[i].Main && modules[i].Path == opts.RootModule {
			rootIndex = i
			break
		}
	}
	if rootIndex < 0 {
		return cyclonedxDocument{}, fmt.Errorf("main module %q was not found in the Go module graph", opts.RootModule)
	}

	unique := make(map[string]module, len(modules))
	for _, item := range modules {
		version := item.Version
		if item.Main {
			version = opts.Version
		}
		key := item.Path + "\x00" + version
		item.Version = version
		unique[key] = item
	}
	items := make([]module, 0, len(unique))
	for _, item := range unique {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Version < items[j].Version
		}
		return items[i].Path < items[j].Path
	})

	root := module{Path: opts.RootModule, Version: opts.Version, Main: true}
	rootRef := cycloneComponentRef(root)
	components := make([]cyclonedxComponent, 0, len(items)-1)
	dependsOn := make([]string, 0, len(items)-1)
	for _, item := range items {
		if item.Main {
			continue
		}
		version := item.Version
		if version == "" {
			version = "NOASSERTION"
		}
		ref := cycloneComponentRef(module{Path: item.Path, Version: version})
		components = append(components, cyclonedxComponent{Type: "library", BomRef: ref, Name: item.Path, Version: version, Scope: "required"})
		dependsOn = append(dependsOn, ref)
	}
	return cyclonedxDocument{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.5",
		SerialNumber: cycloneSerialNumber(opts.Namespace),
		Version:      1,
		Metadata: cyclonedxMetadata{
			Timestamp: opts.Created.UTC().Format(time.RFC3339),
			Tools:     []cyclonedxTool{{Vendor: "AutoGit", Name: "autogit-sbom", Version: "1"}},
			Component: cyclonedxComponent{Type: "application", BomRef: rootRef, Name: root.Path, Version: root.Version},
		},
		Components:   components,
		Dependencies: []cyclonedxDependency{{Ref: rootRef, DependsOn: dependsOn}},
	}, nil
}

func cycloneComponentRef(item module) string {
	digest := sha256.Sum256([]byte(item.Path + "\x00" + item.Version))
	return "autogit:component:" + hex.EncodeToString(digest[:12])
}

func cycloneSerialNumber(namespace string) string {
	digest := sha256.Sum256([]byte(namespace))
	serial := digest[:16]
	serial[6] = (serial[6] & 0x0f) | 0x40
	serial[8] = (serial[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(serial)
	return "urn:uuid:" + hexValue[0:8] + "-" + hexValue[8:12] + "-4" + hexValue[13:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32]
}

func packageID(item module) string {
	version := item.Version
	if version == "" {
		version = "NOASSERTION"
	}
	digest := sha256.Sum256([]byte(item.Path + "\x00" + version))
	return "SPDXRef-Package-" + hex.EncodeToString(digest[:12])
}
