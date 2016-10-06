package kabuta

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

var (
	onceEnv    sync.Once
	onceConfig sync.Once
	// Holds environment variables
	environ map[string]string
)

func returnErrorf(s string, args ...interface{}) gdbMiResponse {
	return gdbMiResponse{err: NewError(s, args)}
}

func returnError(err error) gdbMiResponse {
	return gdbMiResponse{err: err}
}

func MakeGdbResult(x interface{}) string {
	switch x.(type) {
	// TODO this really calls for a default fallthrough behavior
	// being a part of switch statement. I've written too many of these...
	case int:
		fallthrough
	case int8:
		fallthrough
	case int16:
		fallthrough
	case int32:
		fallthrough
	case int64:
		return f("\"%d\"", x)
	case string:
		return f("\"%s\"", x)
	default:
		t := reflect.TypeOf(x)
		switch t.Kind() {
		case reflect.Map:
			panic("Don't know how to deal with %T %s", x, t)
		case reflect.Array:
			panic("Don't know how to deal with %T %s", x, t)
		case reflect.Slice:
			panic("Don't know how to deal with %T %s", x, t)
		case reflect.Struct:
			panic("Don't know how to deal with %T %s", x, t)
		default:
			panic("Don't know how to deal with %T %s", x, t)
		}
	}
}

// Environ is similar to os.Environ() but
// returning environment as a map instead of an
// array of strings.
func Environ() map[string]string {
	onceEnv.Do(initEnviron)
	return environ
}

// Simple utility to provide a string blabla
func String(x interface{}) string {
	// TODO string
	b, e := json.Marshal(x)
	if e == nil {
		return string(b)
	} else {
		return f("%+v", x)
	}
}

func initEnviron() {
	environ = make(map[string]string)
	for _, kv := range os.Environ() {
		keyValue := strings.Split(kv, "=")
		environ[keyValue[0]] = keyValue[1]
	}
}

// Holds config
var config map[string]string

// Config returns Kabuta's configuration -- which consists
// of the environment variable values, overridden with
// values from ~/.kabutainit. If values are not found, they default
// as follows:
// EnvKabutaLogFile: to ~/kabuta.log
func Config() (map[string]string, error) {
	var err error
	onceConfig.Do(func() {
		err = initConfig()
	})
	return config, err
}

func initConfig() error {
	var err error
	env := Environ()
	config = make(map[string]string)
	envVars := []string{EnvKabutaDlvPath, EnvKabutaLogFile, EnvKabutaDlvPort, EnvKabutaPath}
	for _, k := range envVars {
		config[k] = env[k]
	}
	user, err := user.Current()
	if err != nil {
		return err
	}
	configFileName := filepath.Join(user.HomeDir, KabutaInitFile)
	// If the file doesn't exist, it's not an error, just don't do anything
	if _, err = os.Stat(configFileName); os.IsNotExist(err) {
		return nil
	}
	configFile, err := os.Open(configFileName)
	if err != nil {
		return err
	}
	defer configFile.Close()
	reader := bufio.NewReader(configFile)
	lineNo := 0
	for {
		lineNo += 1
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		// Ignore empty lines
		if line == "" {
			continue
		}
		// Ignore comments
		if strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.Split(line, "=")
		if len(kv) < 2 {
			return NewError("Error in %s on line %d: cannot parse %s", configFileName, lineNo, line)
		}
		key := kv[0]
		value := strings.Join(kv[1:], "=")
		config[key] = value
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if config[EnvKabutaLogFile] == "" {
		config[EnvKabutaLogFile] = filepath.Join(user.HomeDir, DefaultKabutaLogFile)
	}
	if config[EnvKabutaDlvPort] == "" {
		config[EnvKabutaDlvPort] = DefaultDlvPort
	}

	return nil
}

// f is a shortcut for fmt.Sprintf
func f(s string, args ...interface{}) string {
	if args == nil || len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

func NewError(err string, args ...interface{}) error {
	if args == nil || len(args) == 0 {
		return errors.New(err)
	}
	msg := f(err, args...)
	out := errors.New(msg)
	return out
}
