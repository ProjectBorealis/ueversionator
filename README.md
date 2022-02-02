# ueversionator

ueversionator is a tool to download custom engine builds based on a uproject's
`EngineAssociation` key. Once downloaded, the engine is extracted to a user specified
base folder and registered for use.

Builds are expected to be archived with 7zip, and both an engine and symbols
archive are supported.

ueversionator is expected to be at the root level of your game project, i.e. with the `.uproject` file.

## Usage examples

Examples on how to use/run ueversionator are provided in the `examples/` folder.

## Command line options

```
Usage of ueversionator:
  -assume-valid
        assumes current archive is valid, if present
  -basedir string
        base directory to download engine bundles in (default "ue4")
  -bundle string
        request UE build bundle (default "editor")
  -config string
        ueversionator config file (default ".ueversionator")
  -ue5
        UE5 build compat
  -user-config string
        ueversionator user config file (default ".uev-user")
  -virgin
        ask configuration options like the first time
  -with-symbols
        include UE engine debug symbols
```

## Configuring ueversionator

### uproject association

A UE uproject file's `EngineAssociation` key needs to be modified with a
`uev:` prefix, followed by the version of the custom build.

```
{
    "FileVersion": 3,
    "EngineAssociation": "uev:4.24-custom",
    "Category": "",
    "Description": "",
    "Modules": [
        ...
    ],
    ...
}
```

### ueversionator config

A simple configuration file is used to tell ueversionator where to fetch builds
from.

```
[ueversionator]
baseurl = https://downloads.example.com/builds
```

ueversionator expects builds to be found under this location, with the
filename `bundlename-<version>.7z`, where the version matches the
`EngineAssociation` key without the `uev:` prefix. So for the
uproject example above, the build would be expected to be found at
`https://downloads.example.com/builds/engine-4.24-custom.7z`.

If `--with-symbols` is used, a debugging symbols archive is expected to be
found with the filename `bundlename-symbols-<version>.7z`.

#### Example engine build

We create our custom builds and archive them with the following commands:

```
# Build Unreal Engine
.\Engine\Build\BatchFiles\RunUAT.bat BuildGraph -Target="Make Installed Build Win64" -Script="Engine/Build/InstalledEngineBuild.xml" -Set:WithDDC=true -Set:HostPlatformEditorOnly=true -Set:WithFeaturePacks=false

# Create archive without debugging symbols
7za.exe a -bsp1 -mx9 -md512m -mfb273 -mlc4 -mmt4 "editor-%VERSION%.7z" "Engine\" "-xr!*.pdb"

# Create archive with debugging symbols
7za.exe a -bsp1 -mx9 -md512m -mfb273 -mlc4 -mmt8 "editor-symbols-%VERSION%.7z" "Engine\**\*.pdb" -r
```
