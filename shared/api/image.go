package api

import (
	"time"
)

// ImagesPost represents the fields available for a new LXD image
type ImagesPost struct {
	ImagePut `yaml:",inline"`

	Filename string            `json:"filename" yaml:"filename"`
	Source   map[string]string `json:"source" yaml:"source"`
}

// ImagePut represents the modifiable fields of a LXD image
type ImagePut struct {
	AutoUpdate bool              `json:"auto_update" yaml:"auto_update"`
	Properties map[string]string `json:"properties" yaml:"properties"`
	Public     bool              `json:"public" yaml:"public"`
}

// Image represents a LXD image
type Image struct {
	ImagePut `yaml:",inline"`

	Aliases      []ImageAlias `json:"aliases" yaml:"aliases"`
	Architecture string       `json:"architecture" yaml:"architecture"`
	Cached       bool         `json:"cached" yaml:"cached"`
	Filename     string       `json:"filename" yaml:"filename"`
	Fingerprint  string       `json:"fingerprint" yaml:"fingerprint"`
	Size         int64        `json:"size" yaml:"size"`
	UpdateSource *ImageSource `json:"update_source,omitempty" yaml:"update_source,omitempty"`

	CreatedAt  time.Time `json:"created_at" yaml:"created_at"`
	ExpiresAt  time.Time `json:"expires_at" yaml:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at" yaml:"last_used_at"`
	UploadedAt time.Time `json:"uploaded_at" yaml:"uploaded_at"`
}

// Writable converts a full Image struct into a ImagePut struct (filters read-only fields)
func (img *Image) Writable() ImagePut {
	return img.ImagePut
}

// ImageAlias represents an alias from the alias list of a LXD image
type ImageAlias struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

// ImageSource represents the source of a LXD image
type ImageSource struct {
	Alias       string `json:"alias" yaml:"alias"`
	Certificate string `json:"certificate" yaml:"certificate"`
	Protocol    string `json:"protocol" yaml:"protocol"`
	Server      string `json:"server" yaml:"server"`
}

// ImageAliasesPost represents a new LXD image alias
type ImageAliasesPost struct {
	ImageAliasesEntry `yaml:",inline"`
}

// ImageAliasesEntryPost represents the required fields to rename a LXD image alias
type ImageAliasesEntryPost struct {
	Name string `json:"name" yaml:"name"`
}

// ImageAliasesEntryPut represents the modifiable fields of a LXD image alias
type ImageAliasesEntryPut struct {
	Description string `json:"description" yaml:"description"`
	Target      string `json:"target" yaml:"target"`
}

// ImageAliasesEntry represents a LXD image alias
type ImageAliasesEntry struct {
	ImageAliasesEntryPut `yaml:",inline"`

	Name string `json:"name" yaml:"name"`
}
