package generate

// Default is the package-level registry that cmd/generate runs. Projects
// register their [OutputEntry] values here, typically from init functions
// in sibling files such as entries_project.go.
//
// Example:
//
//	func init() {
//	    Default.Register(OutputEntry{
//	        Path:   "config.default.toml",
//	        Inputs: []string{"internal/config/*.go"},
//	        Generate: TOMLConfig{
//	            ProjectName: "myapp",
//	            Defaults:    config.DefaultConfig(),
//	            Docs:        config.ConfigDocs,
//	        }.Generate,
//	    })
//	}
var Default = &Registry{}
