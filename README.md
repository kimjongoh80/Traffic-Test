# Traffic-Test

네트워크 장비 양 끝단에 PC 두 대를 연결해 트래픽 스트레스 테스트를 하는 도구입니다.
jperf/iperf를 참고했고, TCP·UDP 대역폭, 지연시간(RTT), 지터, 패킷 손실을 한 번에 측정합니다.
목표 성능은 1G 기준, 25G까지 대응입니다. Windows와 macOS를 지원합니다.

> 1단계 측정 엔진, 2단계 웹 GUI, 3단계 보고서 출력, 4단계 Windows·mac 패키징 완료.
> 자세한 사용법은 [사용설명서](packaging/UserGuide.html)를 보세요. (배포 zip에도 들어 있습니다)

## 구성

```
[PC A: ttest server] ──── 시험 대상 장비(스위치/방화벽/회선) ──── [PC B: ttest client]
```

- **server**: 한쪽 PC에서 대기합니다.
- **client**: 반대쪽 PC에서 테스트를 시작하고 결과를 출력합니다.
- 기본은 client → server 방향 전송, `-R` 옵션으로 server → client(역방향) 전송.

## 내려받기 / 설치

| 파일 | 대상 |
|---|---|
| `TrafficTest-<버전>-windows-x64.zip` | 일반 Windows 10/11 PC |
| `TrafficTest-<버전>-windows-arm64.zip` | ARM Windows 노트북 |
| `TrafficTest-<버전>-macos-apple-silicon.zip` | M1 이상 Mac |
| `TrafficTest-<버전>-macos-intel.zip` | Intel Mac |

- **Windows**: 압축을 풀고 `TrafficTest.exe` 실행(앱 창). 명령줄은 `ttest.exe`. `Allow-Firewall.bat`을 한 번 실행해 방화벽을 엽니다.
- **macOS**: 압축을 풀고 `TrafficTest.app`을 **Control+클릭 → 열기**로 처음 실행합니다(서명되지 않은 앱). 명령줄용 `ttest`도 함께 들어 있습니다.
- 끝낼 때는 화면 오른쪽 위 **종료** 버튼. 이미 실행 중일 때 다시 실행하면 기존 화면을 다시 엽니다.

GitHub Actions가 push마다 테스트와 패키지 빌드를 하고, `v0.4.0`처럼 태그를 push하면 GitHub Release에 zip을 올립니다.

## 빌드

패키지 만들기 (`dist/`): `./scripts/package.sh windows` (어느 OS에서나), `./scripts/package.sh macos` (Mac에서, Xcode 명령줄 도구 필요). Go, zip, [go-winres](https://github.com/tc-hib/go-winres) 필요. GitHub Actions는 Ubuntu에서 Windows용, macOS 러너에서 mac용을 만듭니다.

테스트: `go test ./...`

실행 파일만 빠르게 만들기:

Go 1.22 이상이 필요합니다.

```sh
./build.sh      # macOS / Linux
build.bat       # Windows
```

`dist/` 폴더에 `ttest-windows-amd64.exe`, `ttest-macos-arm64`, `ttest-macos-amd64`가 만들어집니다.

## 앱 화면 (기본)

실행 파일을 **더블클릭**하면 앱 창이 열립니다(Windows: WebView2, macOS: 네이티브 창. 안 되면 기본 브라우저).

1. **Server PC**(트래픽을 보내는 쪽): Mode를 **Server**로 두고 **서버 시작**. Client IP 입력은 필요 없고, Client가 테스트를 시작하기 전까지 트래픽을 보내지 않습니다.
2. **Client PC**(트래픽을 받는 쪽): Mode를 **Client**로 바꾸고 Server 화면에 표시된 IP를 입력한 뒤 **테스트 시작**.
3. 테스트 중에는 양쪽 화면 모두 실시간 그래프가 나오고, 결과는 양쪽 이력에 저장됩니다.

화면 구성:

- **실시간 지표**: 대역폭, 지연시간(RTT), 지터, 패킷 손실을 1초마다 갱신
- **실시간 그래프 4개**: 합격 기준선이 빨간 점선으로 표시됩니다
- **합격 기준**: 최소 대역폭, 최대 손실률, 최대 RTT, 최대 지터를 넣으면 테스트 후 PASS/FAIL 판정
- **스트림별 결과**, **구간별 결과** 표
- **테스트 이력**: 결과는 실행 파일 옆 `results` 폴더에 JSON으로 자동 저장되고, 클릭하면 다시 볼 수 있습니다. 저장 위치는 이력 패널에서 바꿀 수 있습니다
- **화면 설정**: 테마(시스템 / 라이트 / 다크)와 크기 선택. 화면은 창 크기에 맞춰 자동으로 줄어듭니다
- **서버 로그**: 상대 PC에서 이 PC로 들어온 테스트 기록

### 보고서

테스트가 끝나면 결과 요약 아래 버튼으로 내보낼 수 있습니다. 이력 표의 **보기 · CSV** 링크로 지난 테스트도 내보낼 수 있습니다.

| 형식 | 내용 |
|---|---|
| 보고서 (HTML → PDF) | 판정, 시험 정보, 결과 요약, 판정 항목, 그래프 4종, 스트림별·구간별 표. **PDF로 저장 / 인쇄** 버튼으로 A4 PDF 저장 |
| CSV | 구간·스트림별 원본 수치. 엑셀에서 한글이 깨지지 않도록 UTF-8 BOM 포함 |
| JSON | 전체 결과 원본 |

GUI 옵션: `ttest gui -p 5210 -listen -results <폴더> -browser -no-browser`

## 명령줄 사용법

```sh
# PC A
ttest server

# PC B: TCP 10초, 스트림 4개
ttest client -c 192.168.1.10 -t 10 -P 4

# UDP 1Gbps, 패킷 손실·지터 확인
ttest client -c 192.168.1.10 -u -b 1G -t 30

# 역방향(서버가 송신), 결과 JSON 저장
ttest client -c 192.168.1.10 -R --json result.json
```

### client 옵션

| 옵션 | 설명 | 기본값 |
|---|---|---|
| `-c HOST` | 서버 주소 (필수) | |
| `-p PORT` | 서버 포트 | 5210 |
| `-u` | UDP 사용 | TCP |
| `-R` | 역방향 (서버 → 클라이언트) | 꺼짐 |
| `-t SEC` | 테스트 시간(초) | 10 |
| `-P N` | 병렬 스트림 수 (10G/25G는 4~8 권장) | 1 |
| `-b RATE` | 목표 전체 대역폭 (예: 500M, 1G, 25G) | TCP 무제한, UDP 1G |
| `-l BYTES` | TCP 쓰기 크기 / UDP 페이로드 크기 | TCP 128K, UDP 1460 |
| `-i SEC` | 결과 출력 간격 | 1 |
| `-L MS` | 지연시간 프로브 간격(ms), 0이면 끔 | 100 |
| `-S` | 스트림별 결과도 출력 | 꺼짐 |
| `--json FILE` | 전체 결과를 JSON으로 저장 | |
| `--report FILE` | HTML 보고서 저장 (브라우저에서 열어 PDF로 저장) | |
| `--csv FILE` | 구간별 결과를 CSV로 저장 | |

### server 옵션

| 옵션 | 설명 | 기본값 |
|---|---|---|
| `-p PORT` | 대기 포트 | 5210 |
| `-B ADDR` | 특정 로컬 주소에만 바인드 | 전체 |

## 측정 항목

| 항목 | 측정 방법 |
|---|---|
| 대역폭 | 수신 측에서 구간별 수신 바이트로 계산 (평균/최소/최대) |
| 지연시간(RTT) | 테스트 중 100ms마다 UDP 프로브를 보내 왕복시간 측정. **부하 상태의 지연**을 보여줍니다 (min/avg/p95/max) |
| 지터 | UDP: RFC 3550 방식 도착 간격 지터. RTT 지터도 함께 표시 |
| 패킷 손실 | UDP: 시퀀스 번호 기반 구간별 손실, 최종은 송신 수 대비 수신 수. 순서 뒤바뀜도 집계 |

## 방화벽 / 포트

| 프로토콜 | 포트 | 용도 |
|---|---|---|
| TCP | 5210 | 제어 연결 + TCP 데이터 |
| UDP | 5210 | 지연시간 프로브 |
| UDP | 5211 ~ 5210+N | UDP 데이터 (스트림 N개) |

서버 PC의 방화벽(Windows Defender 방화벽, macOS 방화벽)에서 위 포트 또는 `ttest` 프로그램을 허용해야 합니다.
방화벽 장비를 시험할 때도 위 포트를 정책에 열어 주세요.

## 25G 테스트 팁

- TCP는 `-P 4` ~ `-P 8`로 병렬 스트림을 늘리세요.
- UDP 고속 테스트는 패킷 처리량이 한계가 되므로 장비와 NIC가 지원하면 점보 프레임(MTU 9000)을 켜고 `-l 8972`를 사용하세요.
- 양쪽 PC의 NIC, PCIe 대역, CPU 성능이 결과에 영향을 줍니다.

## 결과 JSON

`--json`으로 저장한 파일에는 테스트 설정, 구간별 결과(스트림별 포함), 요약이 들어 있습니다.
3단계 보고서 출력과 2단계 웹 GUI가 이 형식을 그대로 사용합니다.

## 소스 구조

```
cmd/ttest/          진입점 (GUI / server / client)
internal/gui/       웹 GUI 서버, 결과 저장소
  web/              화면 (HTML/CSS/JS, 외부 라이브러리 없이 오프라인 동작)
internal/report/    HTML 보고서(SVG 그래프), CSV 내보내기
internal/engine/    측정 엔진 (CLI와 GUI가 공유)
packaging/          아이콘, Windows 리소스, mac Info.plist, 방화벽 스크립트, 사용설명서
scripts/package.sh  배포 zip 생성
  criteria.go       합격/불합격 판정
  protocol.go       제어 프로토콜, 테스트 설정
  server.go         서버
  client.go         클라이언트, 결과 요약
  traffic.go        TCP/UDP 송수신기, 속도 제어(pacer)
  latency.go        RTT 프로브
  stats.go          구간 통계, 결과 형식
  units.go          단위 변환
```
