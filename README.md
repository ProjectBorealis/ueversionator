# ue4versionator

ue4versionator is a tool to download custom engine builds based on a uproject's
`EngineAssociation` key. Once downloaded, the engine is extracted to `~/.ue4/`
and registered for use.

Builds are expected to be archived with 7zip, and both an engine and symbols
archive are supported.

```
Usage of ue4versionator:
  -config string
        ue4versionator config file (default ".ue4versionator")
  -user-config string
        ue4versionator user config file (default ".ue4v-user")
  -virgin
        ask configuration options like the first time
  -with-engine
        download and unpack UE4 engine build (default true)
  -with-symbols
        download and unpack UE4 engine debug symbols
```

## Configuring ue4versionator

### uproject association

A UE4 uproject file's `EngineAssociation` key needs to be modified with a
`ue4v:` prefix, followed by the version of the custom build.

```
{
    "FileVersion": 3,
    "EngineAssociation": "ue4v:4.21-custom",
    "Category": "",
    "Description": "",
    "Modules": [
        ...
    ],
    ...
}
```

### ue4versionator config

A simple configuration file is used to tell ue4versionator where to fetch builds
from.

```
[ue4versionator]
baseurl = https://downloads.example.com/builds
```

ue4versionator expects builds to be found under this location, with the
filename `engine-<version>.7z`, where the version matches the
`EngineAssociation` key without the `ue4v:` prefix. So for the
uproject example above, the build would be expected to be found at
`https://downloads.example.com/builds/engine-4.21-custom.7z`.

If `--with-symbols` is used, a debugging symbols archive is expected to be
found with the filename `symbols-<version>.7z`.

#### Example engine build
We create our custom builds and archive them with the following commands:

```
# Build UE4 engine
.\Engine\Build\BatchFiles\RunUAT.bat BuildGraph -Target="Make Installed Build Win64" -Script="Engine/Build/InstalledEngineBuild.xml" -Set:WithWin32=false -Set:WithAndroid=false -Set:WithHTML5=false -Set:WithLumin=false -Set:WithFeaturePacks=false -CompatibleChange=%CHANGELIST%

# Create archive without debugging symbols
7z.exe a -bsp1 -mx9 -md512m -mfb273 -mlc4 -mmt24 engine-%VERSION%.7z LocalBuilds\Engine\Windows\Engine\ -r -x^!*.pdb

# Create archive with debugging symbols
7z.exe a -bsp1 -mx9 -md512m -mfb273 -mlc4 -mmt24 symbols-%VERSION%.7z LocalBuilds\Engine\Windows\*.pdb -r
```
