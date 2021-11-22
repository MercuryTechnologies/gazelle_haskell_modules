// Functions used for dependency resolution
package gazelle_haskell_modules

import (
	"fmt"

	//"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	//"github.com/bazelbuild/bazel-gazelle/language"
	//golang "github.com/bazelbuild/bazel-gazelle/language/go"
	//"github.com/bazelbuild/bazel-gazelle/language/proto"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
	//"github.com/bazelbuild/bazel-gazelle/walk"

	"log"
	"path/filepath"
	"sort"
	"strings"
)

// Note [haskell_module naming scheme]
//
// haskell_module rules generated by gazelle_haskell_modules are named
// as <lib>.<module_name>. <lib> is the name of the rule that originated
// the haskell_module rule, and <module_name> is the Haskell module name.
// e.g.
//
// haskell_library(
//     name = "lib",
//     srcs = ["src/A/B/C.hs"]
//     deps = ...,
// )
//
// Produces a haskell_module rule with the name "lib.A.B.C".
//
// The <lib> prefix is used to link the haskell_module rule with the
// library that originated it when determining the labels of rules
// that produce an imported module.
//
// == Alternatives
//
// It was considered changing the implementation so the name of a rule
// is irrelevant when resolving imports, however it was deemed too
// complicated at the time.
//
// The task to solve is determining the label of the library that defines
// a module given either the module filepath or the module name.
//
// This could be accomplished with the rule index, if library rules were
// indexed by both the filepaths and the module names of the modules they
// define. Which in turn would require having the module names and filepaths
// handy when indexing the rule. Which in turn would require putting this
// information in private module attributes when generating the rules.
//
// The above plan would work fine when generating rules anew. But it wouldn't
// be sufficient when updting existing rules. An existing Haskell library
// could have both source files and dependencies on haskell_module rules.
// In order to gather the module names and filepaths, we would need to collect
// the haskell_module rules that appear in the dependencies. And each
// haskell_module rule collected in this way would need to be augmented
// with the module name.
//
// On balance, it is some non-trivial code to add, for a task we can
// already accomplish by setting a convention for our rule names.


// This is to be called only on haskell_library, haskell_binary,
// or haskell_test rules.
//
// Adds to the deps attribute the labels of all haskell_module
// rules originated from this rule.
//
// Removes dependencies defined in the same repo. haskell_module rules
// will depend on the modules of those dependencies instead.
func setNonHaskellModuleDepsAttribute(
	c *Config,
	repoRoot string,
	ix *resolve.RuleIndex,
	r *rule.Rule,
	importData *HRuleImportData,
	from label.Label,
) {
	modules := importData.Modules
	for _, f := range importData.Srcs {
		mod, err := findModuleLabelByModuleFilePath(repoRoot, ix, f, r.Name(), from)
		if err != nil {
			log.Fatal("On rule ", label.New(from.Repo, from.Pkg, r.Name()), ": ", err)
		}
		if mod == nil {
			log.Fatal("On rule ", label.New(from.Repo, from.Pkg, r.Name()), ": couldn't find haskell_module rule for source ", f)
		}
		modules[*mod] = true
	}
	moduleStrings := make([]string, len(modules))
	i := 0
	for lbl, _ := range modules {
		moduleStrings[i] = rel(lbl, from).String()
		i++
	}
	sort.Strings(moduleStrings)

	deps := make([]string, 0, len(importData.Deps))
	for dep, _ := range importData.Deps {
		deps = append(deps, rel(dep, from).String())
	}

	SetArrayAttr(r, "deps", deps)
	SetArrayAttr(r, "modules", moduleStrings)
}

// Sets as deps the labels of all imported modules.
// If the origin of an imported module can't be determined, it
// is ignored.
func setHaskellModuleDepsAttribute(
	ix *resolve.RuleIndex,
	r *rule.Rule,
	importData *HModuleImportData,
	from label.Label,
) {
	originalComponentName := importData.OriginatingRule.Name()
	depsCapacity := len(importData.ImportedModules)
	deps := make([]string, 0, depsCapacity)
	for _, mod := range importData.ImportedModules {
		dep, err := findModuleLabelByModuleName(ix, importData.Deps, mod, originalComponentName, from)
		if err != nil {
			log.Fatal("On rule ", r.Name(), ": ", err)
		}
		if dep == nil {
			continue
		}
		deps = append(deps, rel(*dep, from).String())
	}

	SetArrayAttr(r, "deps", deps)
}

// Yields the label of a module with the given name.
//
// The label is chosen according to the first of the following
// criteria that is met:
//
// 1. If mapDep contains only one dependency of the form <pkg>.<the_module_name>,
// it is chosen.
//
// 2. If importing module comes from the same component (originalComponentName)
// as the given moduleName, the rule defining the module for the given component is
// chosen.
//
// 3. If multiple rules define the module, an error is returned.
//
// 4. If no rule defines the module, nil is returned.
//
func findModuleLabelByModuleName(
	ix *resolve.RuleIndex,
	mapDep map[label.Label]bool,
	moduleName string,
	originalComponentName string,
	from label.Label,
) (*label.Label, error) {
	spec := resolve.ImportSpec{gazelleHaskellModulesName, "module_name:" + moduleName}
	res := ix.FindRulesByImport(spec, gazelleHaskellModulesName)

	var finalLabel *label.Label
	for _, r := range res {
		if _, ok := mapDep[r.Label]; ok {
			if finalLabel == nil {
				lbl := rel(r.Label, from)
				finalLabel = &lbl
			} else {
				return nil, fmt.Errorf("Multiple rules define %s in dependencies: %v %v", moduleName, *finalLabel, r.Label)
			}
		}
	}
	if finalLabel != nil {
		return finalLabel, nil
	}

	for _, r := range res {
		// Here we assume <library>.<module_name> scheme for haskell_module rule names
		// See Note [haskell_module naming scheme].
		pkgName := strings.SplitN(r.Label.Name, ".", 2)[0]
		pkgLabel := label.New(r.Label.Repo, r.Label.Pkg, pkgName)
		originLabel := label.New(from.Repo, from.Pkg, originalComponentName)
		if _, ok := mapDep[pkgLabel]; ok {
			if originLabel != pkgLabel {
				continue
			}
			if finalLabel == nil {
				lbl := rel(r.Label, from)
				finalLabel = &lbl
			} else {
				return nil, fmt.Errorf("Multiple rules define %s in %v: %v %v", moduleName, originLabel, *finalLabel, r.Label)
			}
		}
	}
	if finalLabel != nil {
		return finalLabel, nil
	}

	return nil, nil
}

func findModuleLabelByModuleFilePath(
	repoRoot string,
	ix *resolve.RuleIndex,
	moduleFilePath string,
	componentName string,
	from label.Label,
) (*label.Label, error) {
	relModuleFilePath, err := filepath.Rel(repoRoot, moduleFilePath)
	if err != nil {
		return nil, fmt.Errorf("Can't make src relative: %q: %v", moduleFilePath, err)
	}

	spec := resolve.ImportSpec{gazelleHaskellModulesName, "filepath:" + relModuleFilePath}
	res := ix.FindRulesByImport(spec, gazelleHaskellModulesName)

	for _, r := range res {
		// Here we assume <library>.<module_name> scheme for haskell_module rule names
		// See Note [haskell_module naming scheme].
		rComponentName := strings.SplitN(r.Label.Name, ".", 2)[0]
		if componentName == rComponentName {
			lbl := rel(r.Label, from)
			return &lbl, nil
		}
	}
	if len(res) > 1 {
		labels := make([]label.Label, len(res))
		for i, r := range res {
			labels[i] = rel(r.Label, from)
		}
		return nil, fmt.Errorf("Multiple rules define %q: %v", moduleFilePath, labels)
	} else if len(res) == 1 {
		lbl := rel(res[0].Label, from)
		return &lbl, nil
	} else {
		return nil, nil
	}
}

// dep must be an absolute Label
func isIndexedNonHaskellModuleRule(ix *resolve.RuleIndex, dep label.Label) bool {
	spec := resolve.ImportSpec{gazelleHaskellModulesName, "label:" + dep.String()}
	res := ix.FindRulesByImport(spec, gazelleHaskellModulesName)

	return len(res) > 0
}

// dep must be an absolute Label
func isIndexedHaskellModuleRule(ix *resolve.RuleIndex, dep label.Label) bool {
	spec := resolve.ImportSpec{gazelleHaskellModulesName, "haskell_module:" + dep.String()}
	res := ix.FindRulesByImport(spec, gazelleHaskellModulesName)

	return len(res) > 0
}

func rel(lbl label.Label, from label.Label) label.Label {
	return lbl.Rel(from.Repo, from.Pkg)
}

// "//package".Abs(repo, pkg) leaves the label unchanged when we
// would need "@repo//package"
func abs(lbl label.Label, repo string, pkg string) label.Label {
	if lbl.Repo == "" {
		if lbl.Pkg == "" {
			return label.New(repo, pkg, lbl.Name)
		} else {
			return label.New(repo, lbl.Pkg, lbl.Name)
		}
	} else {
		lbl.Relative = false
		return lbl
	}
}

func SetArrayAttr(r *rule.Rule, attrName string, arr []string) {
    if len(arr) > 0 {
        r.SetAttr(attrName, arr)
    }
}
