package flyctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/superfly/flyctl/helpers"
)

type ConfigFormat string

const (
	TOMLFormat        ConfigFormat = ".toml"
	UnsupportedFormat              = ""
)

type AppConfig struct {
	AppName string
	Build   *Build

	Definition map[string]interface{}
}

type Build struct {
	Builder string
	Args    map[string]string
}

func NewAppConfig() *AppConfig {
	return &AppConfig{
		Definition: map[string]interface{}{},
	}
}

func LoadAppConfig(configFile string) (*AppConfig, error) {
	fullConfigFilePath, err := filepath.Abs(configFile)
	if err != nil {
		return nil, err
	}

	appConfig := AppConfig{
		Definition: map[string]interface{}{},
	}

	file, err := os.Open(fullConfigFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	switch ConfigFormatFromPath(fullConfigFilePath) {
	case TOMLFormat:
		err = appConfig.unmarshalTOML(file)
	default:
		return nil, errors.New("Unsupported config file format")
	}

	return &appConfig, err
}

func (ac *AppConfig) HasDefinition() bool {
	return len(ac.Definition) > 0
}

func (ac *AppConfig) WriteTo(w io.Writer, format ConfigFormat) error {
	switch format {
	case TOMLFormat:
		return ac.marshalTOML(w)
	}

	return fmt.Errorf("Unsupported format: %s", format)
}

func (ac *AppConfig) unmarshalTOML(r io.Reader) error {
	var data map[string]interface{}

	if _, err := toml.DecodeReader(r, &data); err != nil {
		return err
	}

	return ac.unmarshalNativeMap(data)
}

func (ac *AppConfig) unmarshalNativeMap(data map[string]interface{}) error {
	if appName, ok := (data["app"]).(string); ok {
		ac.AppName = appName
	}
	delete(data, "app")

	if buildConfig, ok := (data["build"]).(map[string]interface{}); ok {
		b := Build{
			Args: map[string]string{},
		}
		for k, v := range buildConfig {
			if k == "builder" {
				b.Builder = fmt.Sprint(v)
			} else if k == "args" {
				if argMap, ok := v.(map[string]interface{}); ok {
					for argK, argV := range argMap {
						b.Args[argK] = fmt.Sprint(argV)
					}
				}
			} else {
				b.Args[k] = fmt.Sprint(v)
			}
		}
		if b.Builder != "" {
			ac.Build = &b
		}
	}
	delete(data, "build")

	ac.Definition = data

	return nil
}

func (ac AppConfig) marshalTOML(w io.Writer) error {
	encoder := toml.NewEncoder(w)

	rawData := map[string]interface{}{
		"app": ac.AppName,
	}

	if ac.Build != nil && ac.Build.Builder != "" {
		buildData := map[string]interface{}{
			"builder": ac.Build.Builder,
		}
		if len(ac.Build.Args) > 0 {
			buildData["args"] = ac.Build.Args
		}
		rawData["build"] = buildData
	}

	if err := encoder.Encode(rawData); err != nil {
		return err
	}

	if len(ac.Definition) > 0 {
		// roundtrip through json encoder to convert float64 numbers to json.Number, otherwise numbers are floats in toml
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(ac.Definition)
		d := json.NewDecoder(&buf)
		d.UseNumber()
		if err := d.Decode(&ac.Definition); err != nil {
			return err
		}

		if err := encoder.Encode(ac.Definition); err != nil {
			return err
		}
	}

	return nil
}

func (ac *AppConfig) WriteToFile(filename string) error {
	if err := helpers.MkdirAll(filename); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return ac.WriteTo(file, ConfigFormatFromPath(filename))
}

const defaultConfigFileName = "fly.toml"

func ResolveConfigFileFromPath(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}

	// path is a file, return
	if filepath.Ext(p) != "" {
		return p, nil
	}

	return path.Join(p, defaultConfigFileName), nil
}

func ConfigFormatFromPath(p string) ConfigFormat {
	switch path.Ext(p) {
	case ".toml":
		return TOMLFormat
	}
	return UnsupportedFormat
}

func ConfigFileExistsAtPath(p string) (bool, error) {
	p, err := ResolveConfigFileFromPath(p)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	return !os.IsNotExist(err), nil
}
