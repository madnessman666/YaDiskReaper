# YaDiskReaper

YaDiskReaper is a tool that designed for recursive folder download from yandex disk.

> [!WARNING]
>  *Do not run multiple instances of the program simultaneously. Aggressive concurrent requests may (likely) trigger Yandex rate-limiting and lead to a temporary IP ban.*

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
- -p - number of parallel download (default 8)

## How to run

1. Download bin from releases or from actions

2. Open a terminal in folder where you saved bin file

3. Run it


## Build

MacOS:

```
GOOS=darwin go build -o ./yadiskreaper-macos.exe .
```

Windows: 
```
GOOS=windows go build -o ./yadiskreaper-win.exe .
```

Linux:

```
GOOS=linux go build -o ./yadiskreaper-win.exe .
```
