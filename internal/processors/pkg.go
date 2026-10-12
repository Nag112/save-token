package processors

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Nag112/save-token/internal/util"
)

// PackageInstallProcessor summarizes pip, Poetry, and uv dependency
// installation output (ports PythonInstallProcessor).
type PackageInstallProcessor struct{ Base }

func NewPackageInstall() *PackageInstallProcessor { return &PackageInstallProcessor{} }

func (p *PackageInstallProcessor) Name() string         { return "python_install" }
func (p *PackageInstallProcessor) Priority() int        { return 24 }
func (p *PackageInstallProcessor) HandlesFailure() bool { return true }

func (p *PackageInstallProcessor) HookPatterns() []string {
	return []string{
		`^(pip3?\s+install|poetry\s+(install|update|add)|uv\s+(pip\s+install|sync))\b`,
		PythonCmd + `\s+-m\s+pip\s+install\b`,
	}
}

var (
	piPipInstallR   = regexp.MustCompile(`\bpip3?\s+install\b|` + PythonCmd + `\s+-m\s+pip\s+install\b`)
	piPoetryR       = regexp.MustCompile(`\bpoetry\s+(install|update|add)\b`)
	piUvR           = regexp.MustCompile(`\buv\s+(pip\s+install|sync)\b`)
	piCollectingR   = regexp.MustCompile(`^\s*Collecting\s+`)
	piDownloadingR  = regexp.MustCompile(`^\s*(Downloading|Using cached)\s+`)
	piProgressR     = regexp.MustCompile(`^\s*━|^\s*\[.*\]\s+\d+%|^\s*\d+\.\d+\s*(kB|MB|GB)`)
	piAlreadyR      = regexp.MustCompile(`^\s*Requirement already satisfied`)
	piInstallingR   = regexp.MustCompile(`^\s*Installing collected packages:`)
	piSuccessR      = regexp.MustCompile(`^\s*Successfully installed\s+(.+)`)
	piResolvingR    = regexp.MustCompile(`^\s*(Resolving dependencies|Updating dependencies)`)
	piPoetryInstR   = regexp.MustCompile(`^\s*(Installing|Updating|Removing)\s+(\S+)\s+\((.+?)\)`)
	piUvResolvedR   = regexp.MustCompile(`^\s*Resolved\s+(\d+)\s+packages?`)
	piUvInstalledR  = regexp.MustCompile(`^\s*(Installed|Uninstalled)\s+(\d+)\s+packages?`)
	piErrorR        = regexp.MustCompile(`\b(error|Error|ERROR|exception|Exception|Could not|cannot|Cannot|FAILED|failed|conflict|Conflict|incompatible)\b`)
	piWarningR      = regexp.MustCompile(`\b(warning|Warning|WARNING|DEPRECATION)\b`)
	piPipListFreeze = regexp.MustCompile(`\bpip3?\s+(list|freeze)\b`)
)

func (p *PackageInstallProcessor) CanHandle(command string) bool {
	if piPipListFreeze.MatchString(command) {
		return false
	}
	return piPipInstallR.MatchString(command) ||
		piPoetryR.MatchString(command) ||
		piUvR.MatchString(command)
}

func (p *PackageInstallProcessor) Process(command, output string, exitCode int) string {
	if strings.TrimSpace(output) == "" {
		return output
	}
	switch {
	case piPoetryR.MatchString(command):
		return processPiPoetry(output)
	case piUvR.MatchString(command):
		return processPiUv(output)
	default:
		return processPiPip(output)
	}
}

func processPiPip(output string) string {
	var result []string
	collectingCount := 0
	downloadingCount := 0
	alreadyCount := 0
	var installedPackages []string
	var errorsList []string
	var warnings []string

	for _, line := range util.SplitLines(output) {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		switch {
		case piCollectingR.MatchString(stripped):
			collectingCount++
		case piDownloadingR.MatchString(stripped) || piProgressR.MatchString(stripped):
			downloadingCount++
		case piAlreadyR.MatchString(stripped):
			alreadyCount++
		case piInstallingR.MatchString(stripped):
			// skip
		case piSuccessR.MatchString(stripped):
			m := piSuccessR.FindStringSubmatch(stripped)
			installedPackages = append(installedPackages, strings.Fields(m[1])...)
		case piErrorR.MatchString(stripped):
			errorsList = append(errorsList, line)
		case piWarningR.MatchString(stripped):
			warnings = append(warnings, line)
		}
	}

	if collectingCount > 0 {
		result = append(result, fmt.Sprintf("[%d packages collected]", collectingCount))
	}
	if downloadingCount > 0 {
		result = append(result, fmt.Sprintf("[%d downloads]", downloadingCount))
	}
	if alreadyCount > 0 {
		result = append(result, fmt.Sprintf("[%d already satisfied]", alreadyCount))
	}

	if len(errorsList) > 0 {
		result = append(result, errorsList...)
	}

	if len(warnings) > 0 {
		shown := warnings
		if len(shown) > 5 {
			shown = shown[:5]
		}
		result = append(result, shown...)
		if len(warnings) > 5 {
			result = append(result, fmt.Sprintf("... (%d more warnings)", len(warnings)-5))
		}
	}

	if len(installedPackages) > 0 {
		result = append(result,
			fmt.Sprintf("Successfully installed %d packages:", len(installedPackages)))
		shown := installedPackages
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for _, pkg := range shown {
			result = append(result, "  "+pkg)
		}
		if len(installedPackages) > 10 {
			result = append(result, fmt.Sprintf("  ... (%d more)", len(installedPackages)-10))
		}
	}

	if len(result) == 0 {
		return output
	}
	return strings.Join(result, "\n")
}

func processPiPoetry(output string) string {
	var result []string
	var installed []string
	var updated []string
	var removed []string
	var errorsList []string
	resolvingSkipped := 0

	for _, line := range util.SplitLines(output) {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if piResolvingR.MatchString(stripped) {
			resolvingSkipped++
			continue
		}
		if m := piPoetryInstR.FindStringSubmatch(stripped); m != nil {
			action, pkg, version := m[1], m[2], m[3]
			switch action {
			case "Installing":
				installed = append(installed, pkg+" ("+version+")")
			case "Updating":
				updated = append(updated, pkg+" ("+version+")")
			case "Removing":
				removed = append(removed, pkg)
			}
			continue
		}
		if piErrorR.MatchString(stripped) {
			errorsList = append(errorsList, line)
		}
	}

	if resolvingSkipped > 0 {
		result = append(result, fmt.Sprintf("[dependency resolution: %d steps]", resolvingSkipped))
	}

	if len(errorsList) > 0 {
		result = append(result, errorsList...)
	}

	if len(installed) > 0 {
		result = append(result, fmt.Sprintf("Installed %d packages:", len(installed)))
		shown := installed
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for _, pkg := range shown {
			result = append(result, "  "+pkg)
		}
		if len(installed) > 10 {
			result = append(result, fmt.Sprintf("  ... (%d more)", len(installed)-10))
		}
	}

	if len(updated) > 0 {
		result = append(result, fmt.Sprintf("Updated %d packages:", len(updated)))
		shown := updated
		if len(shown) > 5 {
			shown = shown[:5]
		}
		for _, pkg := range shown {
			result = append(result, "  "+pkg)
		}
		if len(updated) > 5 {
			result = append(result, fmt.Sprintf("  ... (%d more)", len(updated)-5))
		}
	}

	if len(removed) > 0 {
		result = append(result, fmt.Sprintf("Removed %d packages", len(removed)))
	}

	if len(result) == 0 {
		return output
	}
	return strings.Join(result, "\n")
}

func processPiUv(output string) string {
	var result []string
	var errorsList []string
	resolved := 0
	installed := 0
	uninstalled := 0
	downloadingCount := 0

	for _, line := range util.SplitLines(output) {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if m := piUvResolvedR.FindStringSubmatch(stripped); m != nil {
			resolved, _ = strconv.Atoi(m[1])
			continue
		}
		if m := piUvInstalledR.FindStringSubmatch(stripped); m != nil {
			count, _ := strconv.Atoi(m[2])
			if m[1] == "Installed" {
				installed = count
			} else {
				uninstalled = count
			}
			continue
		}
		if piDownloadingR.MatchString(stripped) || piProgressR.MatchString(stripped) {
			downloadingCount++
			continue
		}
		if piErrorR.MatchString(stripped) {
			errorsList = append(errorsList, line)
		}
	}

	if resolved > 0 {
		result = append(result, fmt.Sprintf("Resolved %d packages", resolved))
	}
	if downloadingCount > 0 {
		result = append(result, fmt.Sprintf("[%d downloads]", downloadingCount))
	}
	if len(errorsList) > 0 {
		result = append(result, errorsList...)
	}
	if installed > 0 {
		result = append(result, fmt.Sprintf("Installed %d packages", installed))
	}
	if uninstalled > 0 {
		result = append(result, fmt.Sprintf("Uninstalled %d packages", uninstalled))
	}

	if len(result) == 0 {
		return output
	}
	return strings.Join(result, "\n")
}
