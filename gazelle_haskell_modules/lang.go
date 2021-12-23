// An extension for gazelle to generate haskell_module rules from haskell rules
package gazelle_haskell_modules

import (
	"flag"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"

	"log"
	"path"
	"path/filepath"
)

////////////////////////////////////////////////////
// gazelle callbacks
////////////////////////////////////////////////////

const gazelleHaskellModulesName = "gazelle_haskell_modules"

type gazelleHaskellModulesLang struct{}

func NewLanguage() language.Language {
	return &gazelleHaskellModulesLang{}
}

func (*gazelleHaskellModulesLang) Name() string { return gazelleHaskellModulesName }

func (*gazelleHaskellModulesLang) RegisterFlags(fs *flag.FlagSet, cmd string, c *config.Config) {}

func (*gazelleHaskellModulesLang) CheckFlags(fs *flag.FlagSet, c *config.Config) error { return nil }

func (*gazelleHaskellModulesLang) KnownDirectives() []string {
	return []string{
	}
}

type Config struct {
}

func (*gazelleHaskellModulesLang) Configure(c *config.Config, rel string, f *rule.File) {
	if f == nil {
		return
	}

	m, ok := c.Exts[gazelleHaskellModulesName]
	var extraConfig Config
	if ok {
		extraConfig = m.(Config)
	} else {
		extraConfig = Config{
		}
	}

	for _, directive := range f.Directives {
		switch directive.Key {
		}
	}
	c.Exts[gazelleHaskellModulesName] = extraConfig
}

var haskellAttrInfo = rule.KindInfo{
	MatchAttrs:    []string{},
	NonEmptyAttrs: map[string]bool{},
	ResolveAttrs: map[string]bool{
		"modules":        true,
		"deps":           true,
		"narrowed_deps":  true,
		"srcs":           true,
	},
}

var haskellModuleAttrInfo = rule.KindInfo{
	MatchAttrs:    []string{},
	NonEmptyAttrs: map[string]bool{},
	ResolveAttrs: map[string]bool{
		"deps":               true,
		"cross_library_deps": true,
		"enable_th":          true,
	},
}

var kinds = map[string]rule.KindInfo{
	"haskell_library": haskellAttrInfo,
	"haskell_binary":  haskellAttrInfo,
	"haskell_test":    haskellAttrInfo,
	"haskell_module":  haskellModuleAttrInfo,
}

func (*gazelleHaskellModulesLang) Kinds() map[string]rule.KindInfo {
	return kinds
}

func (*gazelleHaskellModulesLang) Loads() []rule.LoadInfo {
	return []rule.LoadInfo{
		{
			Name:    "@rules_haskell//haskell:defs.bzl",
			Symbols: []string{"haskell_binary", "haskell_library", "haskell_test"},
		},
		{
			Name:    "@rules_haskell//haskell/experimental:defs.bzl",
			Symbols: []string{"haskell_module"},
		},
	}
}

func (*gazelleHaskellModulesLang) Imports(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	if r.Kind() == "haskell_module" {
		originatingRules := getOriginatingRules(r)
		moduleSpecs := make([]resolve.ImportSpec, len(originatingRules), len(originatingRules) + 1)
		for i, originatingRule := range originatingRules {
			moduleSpecs[i] = moduleByFilepathSpec(f.Pkg, originatingRule.Name(), getSrcFromRule(c.RepoRoot, f.Path, r))
		}
		if len(originatingRules) > 0 {
			moduleSpecs = append(moduleSpecs, moduleByNameSpec(getModuleNameFromRule(r)))
		}
		return moduleSpecs
	} else if isNonHaskellModule(r.Kind()) {
		modules := r.PrivateAttr(PRIVATE_ATTR_MODULE_LABELS)
		moduleLabels := map[label.Label]bool{}
		if modules != nil {
			moduleLabels = modules.(map[label.Label]bool)
		}
		libraryDeps := r.PrivateAttr(PRIVATE_ATTR_DEP_LABELS)
		libraryDepLabels := map[label.Label]bool{}
		if libraryDeps != nil {
			libraryDepLabels = libraryDeps.(map[label.Label]bool)
		}

		moduleSpecs := make([]resolve.ImportSpec, len(moduleLabels) + len(libraryDepLabels) + 1)
		i := 0
		for moduleLabel := range moduleLabels {
			moduleSpecs[i] = libraryOfModuleSpec(moduleLabel)
			i++
		}
		i = 0
		for libLabel := range libraryDepLabels {
			moduleSpecs[len(moduleLabels) + i] = isDepOfLibrarySpec(libLabel, f.Pkg, r.Name())
			i++
		}
		moduleSpecs[len(moduleSpecs) - 1] = libraryUsesModulesSpec(label.New(c.RepoName, f.Pkg, r.Name()))
		return moduleSpecs
	} else {
		return []resolve.ImportSpec{}
	}
}

func (*gazelleHaskellModulesLang) Embeds(r *rule.Rule, from label.Label) []label.Label { return nil }

func (*gazelleHaskellModulesLang) Resolve(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, r *rule.Rule, imports interface{}, from label.Label) {
	hmc := c.Exts[gazelleHaskellModulesName].(Config)
	if isNonHaskellModule(r.Kind()) {
		setNonHaskellModuleDeps(&hmc, c.RepoRoot, ix, r, imports.(*HRuleImportData), from)
	} else {
		setHaskellModuleDeps(ix, r, imports.(*HModuleImportData), from)
	}
}

func (*gazelleHaskellModulesLang) GenerateRules(args language.GenerateArgs) language.GenerateResult {
	if args.File == nil {
		return language.GenerateResult{
			Gen:     []*rule.Rule{},
			Imports: []interface{}{},
		}
	}

	ruleInfos := rulesToRuleInfos(args.Dir, args.File.Rules, args.Config.RepoName, args.File.Pkg)
	generateResult := infoToRules(args.Dir, ruleInfos)

	setVisibilities(args.File, generateResult.Gen)

	c := args.Config.Exts[gazelleHaskellModulesName].(Config)
	return addNonHaskellModuleRules(&c, args.Dir, args.Config.RepoName, args.File.Pkg, generateResult, args.File.Rules)
}

func (*gazelleHaskellModulesLang) Fix(c *config.Config, f *rule.File) {
	if !c.ShouldFix || f == nil {
		return
	}

	ruleInfos := rulesToRuleInfos(path.Dir(f.Path), f.Rules, c.RepoName, f.Pkg)

	ruleNameSet := make(map[string]bool, len(ruleInfos))
	for _, info := range ruleInfos {
		rName := ruleNameFromRuleInfo(info)
		ruleNameSet[rName] = true
	}

	for _, r := range f.Rules {
		if !r.ShouldKeep() && r.Kind() == "haskell_module" {
			if _, ok := ruleNameSet[r.Name()]; !ok {
				r.Delete()
			}
		}
	}
	f.Sync()
}


////////////////////////////////
// Indexing
////////////////////////////////

func getModuleNameFromRule(r *rule.Rule) string {
	if r.PrivateAttr(PRIVATE_ATTR_MODULE_NAME) == nil {
		log.Fatal("Error reading module name of " + r.Name())
	}
	return r.PrivateAttr(PRIVATE_ATTR_MODULE_NAME).(string)
}

func getSrcFromRule(repoRoot string, buildFilePath string, r *rule.Rule) string {
	if "" == r.AttrString("src") {
		log.Fatal("Couldn't read src from rule: " + r.Name())
	}
	src, err := filepath.Rel(repoRoot, path.Join(path.Dir(buildFilePath), r.AttrString("src")))
	if err != nil {
		log.Fatal("Reading src of " + r.Name(), err)
	}
	return src
}

func getOriginatingRules(r *rule.Rule) []*rule.Rule {
	v := r.PrivateAttr(PRIVATE_ATTR_ORIGINATING_RULE)
	if v != nil {
		return v.([]*rule.Rule)
	}
	return nil
}
