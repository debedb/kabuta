// Copyright (c) 2016 Gregory Golberg (grisha@alum.mit.edu)
//
// This software is licensed under MIT License:
//
// https://opensource.org/licenses/MIT

// See README.md file for information.

package kabuta

import (
	"github.com/derekparker/delve/service/api"
	"io"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type kabuta struct {
	// Current working directory
	cwd             string
	logFile         *os.File
	frontendChannel chan string
	dlvChannel      chan string
	// Regexp for MI commands
	miCmdRegexp *regexp.Regexp
	// Regexp for CLI commands
	cliCmdRegexp *regexp.Regexp

	breakpointPending bool
	dlvPath           string
	dlvPort           uint64
	dlvCmd            *exec.Cmd
	dlvRpcClient      *rpc.Client
	dlvStdout         io.ReadCloser
	dlvStderr         io.ReadCloser
	// The binary we are debugging
	debugBinaryPath string
	// Where the Go package is
	debugBinaryPackageDir string
	// Arguments to the binary
	debugBinaryArgs         string
	debugBinaryToPackageDir map[string]string
	breakpoints             []*breakpoint
}

// readLoop receives data from the frontend (by reading from stdin)
// and sends this information on the frontendChannel.
func (s *kabuta) frontendReadLoop() {
	defer wg.Done()
	b := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil {
			panic(err)
		}
		if n > 0 {
			str := strings.TrimSpace(string(b[0:n]))
			s.log("RECEIVED %d bytes FROM FRONTEND:\n----------------\n[%s]\n------------------", n, str)
			strArr := strings.Split(str, "\n")
			for _, str2 := range strArr {
				str2 = strings.TrimSpace(str2)
				if str2 == "" {
					continue
				}
				s.frontendChannel <- str2
			}
			b = make([]byte, 1024)
		}
	}
}

func (k *kabuta) dlvReadLoop(stdout bool) {
	buf := make([]byte, 1024)
	eof := false
	var stream io.ReadCloser
	var streamType string
	if stdout {
		stream = k.dlvStdout
		streamType = "stdout"
	} else {
		stream = k.dlvStderr
		streamType = "stderr"
	}
	prev := ""
	for {
		if !eof {
			n, err := stream.Read(buf)
			if n > 0 {
				outStr := string(buf[0:n])
				k.log("RECEIVED %d bytes FROM DLV %d %s:\n---------------------\n[%s]\n---------------------", n, k.dlvCmd.Process.Pid, streamType, outStr)
				out := prev + outStr
				prev = ""
				lines := strings.Split(out, "\n")
				for _, line := range lines[0 : len(lines)-1] {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					k.log("SENDING %d bytes TO DLV CHANNEL [%s]", len(line), line)
					k.dlvChannel <- line
				}
				lastLine := lines[len(lines)-1]
				if strings.HasSuffix(out, "\n") {
					lastLine = strings.TrimSpace(lastLine)
					if lastLine != "" {
						k.log("SENDING %d bytes TO DLV CHANNEL [%s]", len(lastLine), lastLine)
						k.dlvChannel <- lastLine
					}
				} else {
					prev = strings.TrimSpace(lastLine)
				}
			}
			if n == 0 && err == io.EOF && k.dlvCmd.ProcessState != nil && k.dlvCmd.ProcessState.Exited() {
				k.log("dlv %s finished: %v", streamType, k.dlvCmd.ProcessState.Sys())
				eof = true
			} else if err != nil && err != io.EOF {
				k.log("Error reading from %s of dlv: %s", streamType, err)
			}
		}
	}
}

// frontendWriteLoop continually checks for messages coming on
// frontendChannel and processes them.
func (k *kabuta) frontendWriteLoop() {
	defer wg.Done()
	for {
		k.writeToFrontend(GdbPrompt)
		str := <-k.frontendChannel
		k.log("RECEIVED %d bytes FROM FRONTEND CHANNEL:\n---------------------\n[%s]\n---------------------", len(str), str)
		req := newFrontendRequest(k, str)
		req.process()
	}
}

// log logs the message (f formats can be used).
func (s *kabuta) log(str string, args ...interface{}) {
	_, err = s.logFile.WriteString(f(str+"\n", args...))
	if err != nil {
		panic(err)
	}
}

// writeToFrontend sends the string to stdout.
func (s *kabuta) writeToFrontend(str string) {
	n, err := os.Stdout.WriteString(str)
	if err != nil {
		s.log("Error sending %s: %v\n", str, err)
		panic(err)
	}
	s.log("SENT: %d bytes:\n----------------\n%s\n------------------", n, str)
}

// frontendRequest holds information about the frontend request that
// will be needed for the response.
type frontendRequest struct {
	kabuta *kabuta
	t0     time.Time
	rawCmd string
	gdbCmd *gdbCmd
	token  string
}

const (
	breakpointTypeLinenum = iota
	breakpointTypeOffset
	breakpointTypeFilenameLinenum
	breakpointTypeFunction
	breakpointTypeFunctionLabel
	breakpointTypeFilenameFunction
	breakpointTypeLabel
)

// https://sourceware.org/gdb/onlinedocs/gdb/Linespec-Locations.html#Linespec-Locations
type breakpoint struct {
	rawLocation    string
	breakpointType int
	fileName       string
	lineNo         int
	function       string
	dlvBreakpoint  *api.Breakpoint
}

func (bp breakpoint) String() string {
	switch bp.breakpointType {
	case breakpointTypeFilenameLinenum:
		return f("Breakpoint at %s:%d, Delve breakpoint: %+v", bp.fileName, bp.lineNo, *bp.dlvBreakpoint)
	case breakpointTypeFunction:
		return f("Breakpoint at function %s, Delve breakpoint: %+v", bp.function, *bp.dlvBreakpoint)
	default:
		return "Impossible breakpoint"
	}
}

// See https://sourceware.org/gdb/onlinedocs/gdb/GDB_002fMI-Breakpoint-Commands.html
func newBreakpoint(rawLocation string) (*breakpoint, error) {
	bp := breakpoint{rawLocation: rawLocation, dlvBreakpoint: &api.Breakpoint{}}
	elts := strings.Split(rawLocation, ":")
	if len(elts) == 1 {
		elt := elts[0]
		// Offset not supported
		if strings.HasPrefix(elt, "+") || strings.HasPrefix(elt, "-") {
			return nil, NewError("Breakpoint location type not supported: %s", rawLocation)
		}
		_, err = strconv.ParseInt(elt, 10, 64)
		if err == nil {
			// This is a line number... Not supported.
			return nil, NewError("Breakpoint location type not supported: %s", rawLocation)
		}
		// Assume it's function
		bp.breakpointType = breakpointTypeFunction
		bp.function = elts[0]
		bp.dlvBreakpoint.FunctionName = elts[0]
	} else {
		if len(elts) != 2 {
			return nil, NewError("Breakpoint location type not supported: %s", rawLocation)
		}
		fileName := elts[0]
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			return nil, NewError("Breakpoint location type not supported: %s (assumed %s is a file but it doesn't exist)", rawLocation, fileName)
		}
		bp.fileName = fileName
		bp.dlvBreakpoint.File = fileName

		lineNo, err := strconv.ParseInt(elts[1], 10, 32)
		if err != nil {
			return nil, NewError("Breakpoint location type not supported: %s (assumed %s is a line number, but cannot parse: %s", rawLocation, elts[1], err)
		}
		bp.breakpointType = breakpointTypeFilenameLinenum
		bp.lineNo = int(lineNo)
		bp.dlvBreakpoint.Line = bp.lineNo
	}
	return &bp, nil
}

// newFrontendRequest creates a new frontendRequest
// object, initializing fields as needed.
func newFrontendRequest(k *kabuta, command string) *frontendRequest {
	return &frontendRequest{kabuta: k, t0: time.Now(), rawCmd: command}
}

// processMiCmd creates miCmd object and invokes its
// functions for processing this MI command.
func (r *frontendRequest) processCmd(matches [][]string, isMiCmd bool) {
	r.token = matches[0][1]
	cmdLine := matches[0][2]
	cmdElts := strings.Split(cmdLine, " ")
	r.gdbCmd = &gdbCmd{cmd: cmdElts[0], frontendRequest: r, isMiCmd: isMiCmd}
	if len(cmdElts) > 1 {
		r.gdbCmd.args = cmdElts[1:]
		r.gdbCmd.argsStr = strings.Join(r.gdbCmd.args, " ")
	}
	r.gdbCmd.process()
}

func (r *frontendRequest) process() {
	miMatches := r.kabuta.miCmdRegexp.FindAllStringSubmatch(r.rawCmd, -1)
	if miMatches != nil && len(miMatches) > 0 {
		r.kabuta.log("%s MATCHES: %v", r.rawCmd, miMatches)
		r.processCmd(miMatches, true)
		return
	}
	cliMatches := r.kabuta.cliCmdRegexp.FindAllStringSubmatch(r.rawCmd, -1)
	if cliMatches != nil && len(cliMatches) > 0 {
		r.kabuta.log("%s MATCHES: %v", r.rawCmd, cliMatches)
		r.processCmd(cliMatches, false)
		return
	}
	r.kabuta.log("Unknown command '" + r.rawCmd + "'")
}

// gdbSummary returns the summary line per GDB MI protocol.
func (r *frontendRequest) gdbSummary() string {
	sec0 := float64(r.t0.UnixNano()) / 1000000.
	sec1 := float64(time.Now().UnixNano()) / 1000000.
	passed := sec1 - sec0
	fakeUserSys := passed / 2
	retval := f(`time={wallclock="%.5f",user="%.5f",system="%.5f",start="%f",end="%f"}`, passed, fakeUserSys, fakeUserSys, sec0, sec1)
	return retval
}

// Run runs the debugger.
func Run() error {
	conf, err := Config()
	if err != nil {
		return err
	}
	k := &kabuta{}

	// Start logging.
	logFileName := conf[EnvKabutaLogFile]
	if logFileName == "" {
		logFileName = "kabuta.log"
	}
	logFileName, err = filepath.Abs(logFileName)
	if err != nil {
		return NewError("Error opening file \"%s\" specified by %s for logging: %v", logFileName, EnvKabutaLogFile, err)
	}
	k.logFile, err = os.OpenFile(logFileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0777)
	if err != nil {
		return NewError("Error opening file \"%s\" specified by %s for logging: %v", logFileName, EnvKabutaLogFile, err)
	}
	defer k.logFile.Close()

	// Check DLV binary
	dlvPath := conf[EnvKabutaDlvPath]
	if dlvPath == "" {
		k.log("Nothing specified by %s, will run \"dlv\"", dlvPath)
		dlvPath = "dlv"
	}
	dlvVer := exec.Command(dlvPath, "version")
	dlvVerOut, err := dlvVer.CombinedOutput()
	if err != nil {
		return NewError("Cannot run dlv version at \"%s\" specified by %s: %s", dlvPath, EnvKabutaDlvPath, err)
	}
	dlvVerStr := string(dlvVerOut)
	if !strings.HasPrefix(dlvVerStr, DlvVersionOutputStart) {
		return NewError("Expected dlv version to start with %s, returned %s", DlvVersionOutputStart, dlvVerStr)
	}
	k.dlvPath = dlvPath
	k.log("dlv version:\n%s", dlvVerStr)

	// Get DLV port
	dlvPortStr := conf[EnvKabutaDlvPort]
	k.dlvPort, err = strconv.ParseUint(dlvPortStr, 10, 32)
	if err != nil {
		return NewError("Expected DLV port specified by %s to be integer, got %s: %s", EnvKabutaDlvPort, dlvPortStr, err)
	}

	// Set path
	kabutaPath := conf[EnvKabutaPath]
	if kabutaPath != "" {
		oldPath := Environ()["PATH"]
		newPath := oldPath + ":" + kabutaPath
		os.Setenv("PATH", newPath)
		k.log("Added %s %s to PATH %s, new value %s", EnvKabutaPath, kabutaPath, oldPath, newPath)
	}

	k.frontendChannel = make(chan string)
	k.dlvChannel = make(chan string)
	k.miCmdRegexp = regexp.MustCompile(RegexpMiCmd)
	k.cliCmdRegexp = regexp.MustCompile(RegexpCliCmd)
	k.debugBinaryToPackageDir = make(map[string]string)
	wg.Add(2)

	args := os.Args[1:]
	k.log("Running with %v\n", args)
	if len(args) == 1 {
		if args[0] == "--version" {
			k.writeToFrontend(GdbVersion + "\n")
			return nil
		}
	}
	go k.frontendReadLoop()
	go k.frontendWriteLoop()
	wg.Wait()
	return nil
}
