package model

type Entry struct {
	ID            string        `json:"id,omitempty"`
	Name          string        `json:"name,omitempty"`
	Description   string        `json:"description,omitempty"`
	Repository    Repository    `json:"repository,omitempty"`
	VersionDetail VersionDetail `json:"version_detail,omitempty"`
}

type Repository struct {
	URL       string `json:"url,omitempty"`
	SubFolder string `json:"subfolder,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
}
type VersionDetail struct {
	Version     string `json:"version,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"` //RFC 3339 date format
	IsLatest    bool   `json:"is_latest,omitempty"`
}

type ServerDetail struct {
	ID                string        `json:"id,omitempty"`
	Name              string        `json:"name,omitempty"`
	Description       string        `json:"description,omitempty"`
	VersionDetail     VersionDetail `json:"version_detail,omitempty"`
	Repository        Repository    `json:"repository,omitempty"`
	RegistryCanonical string        `json:"registry_canonical,omitempty"`
	Registries        []Registries  `json:"registries,omitempty"`
	Remotes           []Remotes     `json:"remotes,omitempty"`
}

type Registries struct {
	Name             string  `json:"name,omitempty"`
	PackageName      string  `json:"package_name,omitempty"`
	License          string  `json:"license,omitempty"`
	CommandArguments Command `json:"command_arguments,omitempty"`
}

type Remotes struct {
	TransportType string `json:"transport_type,omitempty"`
	Url           string `json:"url,omitempty"`
}

type Command struct {
	SubCommands          []SubCommand          `json:"sub_commands,omitempty"`
	PositionalArguments  []PositionalArgument  `json:"positional_arguments,omitempty"`
	EnvironmentVariables []EnvironmentVariable `json:"environment_variables,omitempty"`
	NamedArguments       []NamedArguments      `json:"named_arguments,omitempty"`
}

type EnvironmentVariable struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Argument struct {
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	IsRequired   bool     `json:"is_required,omitempty"`
	IsEditable   bool     `json:"is_editable,omitempty"`
	IsRepeatable bool     `json:"is_repeatable,omitempty"`
	Choices      []string `json:"choices,omitempty"`
}

type PositionalArgument struct {
	Position int      `json:"position,omitempty"`
	Argument Argument `json:"argument,omitempty"`
}

type SubCommand struct {
	Name           string           `json:"name,omitempty"`
	Description    string           `json:"description,omitempty"`
	NamedArguments []NamedArguments `json:"named_arguments,omitempty"`
}
type NamedArguments struct {
	ShortFlag     string   `json:"short_flag,omitempty"`
	LongFlag      string   `json:"long_flag,omitempty"`
	RequiresValue bool     `json:"requires_value,omitempty"`
	Argument      Argument `json:"argument,omitempty"`
}
