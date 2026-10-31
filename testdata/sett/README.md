# Set T

Set T is the regression set of AX11.2. Each file is a small TeX file that reproduces one pattern of AX4.9. A person writes it. It is not the source of a paper.

The `% want` lines at the top give the expected objects, in the order of the paper:

```
% want <local> <item> <number or -> [proves=<local>] [part=<marker>] [status=<proof_status>] [origin=<origin>] [labels=<a,b>] [parent=<local>] [flag=<flag>] [lines=<from>-<to>]
```

A key that the line does not give is not checked. `lines` is the first and the last line of the object. The test also checks that the census of the file has no gap and no phantom. A `% finding <rule>` line says that the census must give a finding of that rule. With no such line, the census must give no finding.

The files of a directory with the name of the test, as `class-option-off/journal.cls`, go with the main file as local files.

When a pattern comes from a paper, a `% from <id>` line names the paper.

Run the set with `go test ./analysis -run TestSetT`.
