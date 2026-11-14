package main

type EnumEntry struct {
	Const   string `yaml:"const"`
	TSConst string `yaml:"ts_const"`
	Value   string `yaml:"value"`
}

type TierDef struct {
	ID           int    `yaml:"id"`
	Name         string `yaml:"name"`
	ShortLive    string `yaml:"short_live"`
	ShortStorage string `yaml:"short_storage"`
}

type DivisionsDef struct {
	ZeroBased       []string `yaml:"zero_based"`
	ZeroBasedShort  []string `yaml:"zero_based_short"`
	OneBasedStorage []string `yaml:"one_based_storage"`
	OneBasedShort   []string `yaml:"one_based_short"`
}

type PlaylistDef struct {
	ID               int      `yaml:"id"`
	Name             string   `yaml:"name"`
	StorageName      string   `yaml:"storage_name"`
	IsRanked         bool     `yaml:"is_ranked"`
	IsExtraMode      bool     `yaml:"is_extra_mode"`
	BallchasingSlugs []string `yaml:"slugs"`
}

type EnumsSchema struct {
	Enums     map[string][]EnumEntry `yaml:"enums"`
	Tiers     []TierDef              `yaml:"tiers"`
	Divisions DivisionsDef           `yaml:"divisions"`
	Playlists []PlaylistDef          `yaml:"playlists"`
}

type StatColumnDef struct {
	Key         string `yaml:"key"`
	Label       string `yaml:"label"`
	ShortLabel  string `yaml:"short_label"`
	Description string `yaml:"description"`
}

type PresetDef struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Columns     []string `yaml:"columns"`
}

type TelemetrySchema struct {
	StatColumns []StatColumnDef      `yaml:"stat_columns"`
	ColumnOrder []string             `yaml:"column_order"`
	Presets     map[string]PresetDef `yaml:"presets"`
}

type FieldDef struct {
	Name      string `yaml:"name"`
	GoType    string `yaml:"go_type"`
	TSType    string `yaml:"ts_type"`
	JSON      string `yaml:"json"`
	Omitempty bool   `yaml:"omitempty"`
}

type DTODef struct {
	Name   string     `yaml:"name"`
	TSName string     `yaml:"ts_name"`
	Doc    string     `yaml:"doc"`
	Fields []FieldDef `yaml:"fields"`
}

type DTOSchema struct {
	DTOs []DTODef `yaml:"dtos"`
}
