# Traffic-Test

네트워크 장비 양 끝단에 PC 두 대를 연결해 트래픽 스트레스 테스트를 하는 도구입니다.
jperf/iperf를 참고했고, TCP·UDP 대역폭, 지연시간(RTT), 지터, 패킷 손실을 한 번에 측정합니다.
목표 성능은 1G 기준, 25G까지 대응입니다. Windows와 macOS를 지원합니다.

> 현재 단계: **1단계 측정 엔진(CLI)** 완료. 웹 GUI(2단계), 보고서 출력(3단계), 패키징(4단계) 예정.

## 구성

```
[PC A: ttest server] ──── 시험 대상 장비(스위치/방화벽/회선) ──── [PC B: ttest client]
```

- **server**: 한쪽 PC에서 대기합니다.
- **client**: 반대쪽 PC에서 테스트를 시작하고 결과를 출력합니다.
- 기본은 client → server 방향 전송, `-R` 옵션으로 server → client(역방향) 전송.

## 빌드

Go 1.22 이상이 필요합니다.

```sh
./build.sh      # macOS / Linux
build.bat       # Windows
```

`dist/` 폴더에 `ttest-windows-amd64.exe`, `ttest-macos-arm64`, `ttest-macos-amd64`가 만들어집니다.

## 사용법

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
cmd/ttest/          CLI 진입점
internal/engine/    측정 엔진 (웹 GUI에서도 재사용)
  protocol.go       제어 프로토콜, 테스트 설정
  server.go         서버
  client.go         클라이언트, 결과 요약
  traffic.go        TCP/UDP 송수신기, 속도 제어(pacer)
  latency.go        RTT 프로브
  stats.go          구간 통계, 결과 형식
  units.go          단위 변환
```
