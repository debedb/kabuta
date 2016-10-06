# Kabuta

Kabuta adapts [Delve's API](https://godoc.org/github.com/derekparker/delve/service/rpc2#RPCServer) to [GDB/MI interface](https://ftp.gnu.org/old-gnu/Manuals/gdb-5.1.1/html_node/gdb_211.html#SEC216) for the purpose of making Delve available to various front-ends (IDE, GUI debugger interfaces, etc.) that already have integration with GDB.

This is currently oriented to my use of [Goclipse](https://goclipse.github.io/) as a primary use case. Other use cases are welcome; it's just that this one is what I am familiar with. 

## Development

This is currently being developed with GoClipse as the main use case, and so also using GoClipse. This is my development setup, as a reference:

 1. In the Eclipse workspace, there is the Kabuta GoClipse project, in whose src sdirectory there is
    github.com/debedb/kabuta pointing to this repo.
 2. Under the repo structure, in testdata/, there are the following projects:
    a. cdtproject -- an Eclipse [CDT](https://eclipse.org/cdt/) project which contains a simple C program   
       to be debugged -- to be used as a reference. If you use Eclipse, then create a project with it 
       (TODO)
    b. goclipseproject -- a GoClipse project containing a Go project to be debugged. The idea is that
       this Go project has enough Go features to try out the concept. If you use Eclipse, the create a
       project with it (TODO)
  
## Running

### Configuring

Because Kabuta is invoked by a front-end which is designed to use GDB, it is limited to being invoked with the same command-line arguments as GDB. Therefore, to configure its behavior environment variables and/or an init file are used. The rules are as follows:

 1. First, the init file is loaded. Init file is `~/.kabutainit`. Its evaluation is simple:
    1. It is considered line by line.
    2. Each line is stripped of surrounding whitespace
    3. If the line is empty it is ignored
    4. If a line starts with # it is ignored (comments)
    5. The line is split on the first = (equal sign). If there is no equal sign, the line is ignored. 
       If there is, whatever is to the left is the key, and whatever is to the right is the value.
    6. Based on the above rules, a key-value map is built. 
 2. The following keys are recognized (TODO link all this to GoDoc):
    1. KABUTA_LOG_FILE - log file. If cannot be opened, or missing, defaults to `~/kabuta.log`. This is
       important, because stdin/stdout/stderr are for communicating with the front-end. Therefore, a file   
       is needed.
    2. KABUTA_DLV_PATH - path to dlv binary
    3. KABUTA_DLV_PORT - port on which dlv API server is to listen. If invalid (not a positive integer) 
       or missing, defaults to 8181.
    4. KABUTA_PATH - its value should be path-separator-separated
  3. Environment variables named same as above keys can override values from the `~/.kabutainit` file  
  (see above).

### How to run with GoClipse

TODO

## Bugs/enhancements/suggestions/questions

Issues and pull requests (though it's a bit too early for the latter at this stage) are welcome. Or drop me an email at [grisha_AT_alum.mit.edu](mailto:grisha@alum.mit.edu).

If something is not clear, feel free to ask.

## License

[MIT License](LICENSE.md)