# YaDiskReaper

YaDiskReaper is a tool that designed for recursive folder download from yandex disk.

## Supported OS:

- Linux
- Windows

## Usage

### Example:

```
.\yadiskreaper-linux -u https://disk.yandex.ru/d/gqlRRlWw2GvZtw -o /path/to/folder 
```
Args explain:

- -u - path to yandex disk folder
- -o - path to destination folder
- -p (not implemented) - number of parallel download 

## How to run

1. Download bin from releases or from actions

2. Open a terminal in folder where you saved bin file

3. Run it


## Build

Windows: 

On windows:

```
go build -o ./yadiskreaper-win.exe .
```

On linux: 

```
GOOS=windows go build -o ./yadiskreaper-win.exe .
```

Linux:

On linux:

```
GOOS=linux go build -o ./yadiskreaper-win.exe .
```

On Windows: (Maybe in some future i will explain how...)