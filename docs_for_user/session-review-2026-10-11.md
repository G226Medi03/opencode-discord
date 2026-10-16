# 세션 회고 — 2026-10-11 · Discord로 OpenCode 제어 (oc-discord, Go)

> 이 문서는 이번 세션 대화 전체를 분석한 기록입니다.
> 1) 오늘 한 일  2) 미리 제시했어야 했던 것 / 세션 개선점  3) 사용자가 요청한 수정사항

---

## 0. 한 줄 요약

기존 Cline 커넥터 이력을 참고해 **OpenCode를 `opencode acp`(ACP)로 제어하는 Go 브리지 `oc-discord`** 를 새로 만들고, 구 스택을 삭제한 뒤, 표기/명령/스크립트/구조/카테고리 허용까지 다듬었다. 마지막에는 모듈을 레포 루트로 올려 `go run .` 으로 실행하게 하고 ps1 을 제거했다.

---

## 1. 오늘 한 일

### 1.1 탐색 · 설계
- 기존 `acp-bot/`(Node, `cline --acp`) 구조·정책·스크립트 분석.
- `opencode acp` 가 ACP v1 stdio 서버임을 실측(`initialize` → loadSession/list/resume/fork/delete 광고).
- 대안 비교(ACP stdio vs HTTP+SSE) 후 **ACP 채택**. `discordgo`, `coder/acp-go-sdk`, `opencode.exe` 경로 확인.

### 1.2 `oc-discord` 신규 구현 (Go)
- 패키지: `config/logx/launcher/acp/agentpool/projects/bindings/store/chunk/perms/discord`(+ 이후 `agent/opencode/chatcmd/bridge`).
- 기능: 채널/스레드=프로젝트(cwd)=세션, 스트리밍 편집, 코드펜스 분할, 권한 버튼, 유휴 종료, `session/resume`, 기동 감사.
- **버그 발견·수정**: ACP `session/request_permission` 응답은 **중첩** 형태여야 함 → `{"outcome":{"outcome":"selected",...}}`. 처음 평면으로 보내 모두 cancelled 되던 것을 실측으로 잡음.
- 프로토콜 실측 도구 `cmd/acp-smoke`.

### 1.3 구 스택 정리
- 삭제: `acp-bot/`, `run/stop-acp-bot.ps1`, 옛 로그. 문서 흔적 정리(README/.env/.env.example/.gitignore/json `_readme`/load-env.ps1/Go 주석).

### 1.4 표기 · 명령 체계
- **모드 표기**: 일반 대화 답변에 `[Plan]`/`[Build]` 헤더(뒤 줄바꿈) + `[답변종료]` 푸터. 에러/커맨드/버튼엔 미적용.
- **`/cmd`**: owner 전용 셸 실행 → `[Cmd] … [종료]`(타임아웃 60s, 출력 100KB 캡).
- **명령 레지스트리 + 공통 디스패치**: 모든 명령에 **텍스트 트리거(`/`와 `!` 둘 다)**, 응답 **전부 공개**.

### 1.5 구조 리팩터 (재사용성)
- `agent` 추상화(`Agent/Sink/Permission/Pool`) + `acp` 구현 + `opencode` factory + `agentpool` 일반화.
- (시도) `chat.Transport` 추상화 → **사용자 요청으로 폐기**: `internal/chat` 삭제, 타입을 `discord` 로 이동, `bridge` 가 `discord` 직접 사용.
- `chatcmd`(범용 명령 레지스트리), `bridge`(글루).

### 1.6 운영 방식 전환
- `.ps1` 4개(`run/stop/restart/load-env`) 삭제.
- **모듈을 레포 루트로 이동** → **`go run .`** 으로 봇 실행. `cmd/acp-smoke` 는 별도 유지.
- config 경로 수정(`bot-config.json` 루트, `state/` 루트), `.gitignore` 갱신, README 단일화.

### 1.7 채널 허용
- 원인 진단: `channel-policy.json` 의 `resolved.channelIds` 에 `ccst` 한 개만 있어 `morbes_go_db` 무응답.
- **카테고리 기준 허용** 구현: `resolved.categoryIds` → `AllowedCategoryIDs`, `bindings.IsAllowed` 가 카테고리/스레드까지 인정.

### 1.8 기타
- `bot-config.json` 에서 파서가 안 읽는 `_` 접두 키 전부 제거.
- 외부 API(OpenCode) 의존 테스트(`acp` 통합 테스트) 삭제.
- (참고) 코드에 외부에서 들어온 리네임(`DiscordBot`→정리, `Get_BotId`, `DiscordClient`, `DiscordClientOption`, `Send_Msg`, `discord_client` 필드 등)을 그대로 따라가며 맞춤.

### 산출물(현재)
```
opencode_discord/
  go.mod · go.sum · main.go            (go run .)
  cmd/acp-smoke/                        (go run ./cmd/acp-smoke)
  internal/{config,logx,launcher,agent,acp,opencode,agentpool,
            projects,bindings,store,chunk,perms,chatcmd,bridge,discord}
  bot-config.json · state/ · logs/
  .env · channel-policy.json · channel-cwd.json · README.md
```

---

## 2. 미리 제시했어야 했던 것 / 세션 개선점

### 2.1 처음에 정했으면 재작업이 줄었던 것

| 항목 | 실제 흐름 | 미리 정했으면 좋았던 형태 |
|---|---|---|
| **명령 실행 인터페이스** | 슬래시로 만들었다가 "왜 `/cmd`가 안 되냐"로 여러 턴 소모 → 텍스트 트리거 전면 추가 | "명령은 **텍스트 `/명령`·`!명령`** 으로도 동작(등록 여부 무관)" 을 처음에 |
| **Discord 슬래시의 전제** | 슬래시는 등록+피커로만 실행됨을 늦게 공유 | "슬래시 쓸지, 텍스트 쓸지"를 첫 요구로 |
| **응답 포맷** | 모드 표기/`/cmd` 포맷을 왕복하며 확정 | 최종 화면 예시(코드블록) 하나로 |
| **접두어 스킴** | `!만` → `!`와 `/` 둘 다 | 처음부터 |
| **채널 허용 범위** | 채널 ID만 봐서 카테고리 채널 무응답 | "카테고리 하위 전체 허용" 을 처음에 |
| **실행 방식** | ps1 → `go run .` 로 뒤늦게 전환 | "`go run .` 로 실행" 을 처음에 |
| **구조/재사용 범위** | 추상화 2축 도입 후 `chat` 인터페이스 폐기 | "인터페이스 없이 discord 직접" 을 처음에 |
| **안전·권한** | owner 전용 vs 패턴차단 vs 버튼확인 왕복 | 초기에 확정 |
| **삭제 범위** | "기존 파일 삭제"가 모호 | 남길 것까지 명시 |

### 2.2 반복을 만든 근본 원인

1. **슬래시 vs 텍스트 전제 차이**: `/cmd dir` 이 왜 OpenCode로 가는지 설명/진단에 턴을 여러 번 씀.
2. **붙여넣은 결과에 "기대와 다른 점" 미기재**: 봇 출력만 붙여서 원인 추정 턴이 추가됨.
3. **한 요구의 여러 제약을 나눠 전달**: 포맷/적용범위/줄바꿈을 왕복.
4. **환경 상태 미공유**: 슬래시 등록 여부, 봇 프로세스 수, 재시작 필요 여부.
5. **추상화 강도/재사용 범위를 늦게 확정**: 만들었다 되돌림(`chat`).

### 2.3 다음 세션용 요청 템플릿 (복붙용)

```
[목표] 최종 사용자 동작 1문장 (예: "Discord `/cmd dir` 치면 그 채널 cwd에서 실행 결과가 온다")
[실행 방식] 슬래시 / 텍스트 / 둘 다? (슬래시 등록 필요 여부 명시)
[출력 형식] 헤더/푸터/코드블록/분할 정확히 (예시 포함)
[권한·안전] owner 전용? 패턴차단? 실행 전 확인?
[범위·부수효과] 삭제/이름변경/재빌드/재시작 필요 여부
[현재 상태] 봇 프로세스 수 / 슬래시 등록 여부 / 최근 변경
[산출물] 코드/문서/스크립트/테스트 중 무엇
```

### 2.4 잘한 점 (유지)
- 명확한 상위 목표(Go, OpenCode) — 기술 선택이 흔들리지 않음.
- 질문에 대한 즉답, 실측 로그 제공(진단에 큰 도움).
- 단계적 확장(브리지 → 정리 → 표기 → 명령 → 구조 → 실행방식).

### 2.5 학습 정리
- Discord 슬래시는 **등록+피커 전제**. 타이핑 습관과 맞추려면 **텍스트 트리거**가 필요.
- 포맷/태그/줄바꿈은 말보다 **화면 예시 하나**.
- 진단 3종: "기대 / 실제 / 실행경로(슬래시·텍스트·등록여부)".
- 파괴적 요청은 **남길 것까지** 명시.

---

## 3. 사용자가 요청한 수정사항 (시간순)

1. **계획 수립 + Go 브리지 구현**: "open_code를 Discord에서 제어, 이전 이력은 참고, 가능하면 Go로" → `oc-discord/` 신규 구현.
2. **기존 파일 삭제**: 구 커넥터 스택 + 관련 문서 흔적까지 삭제(`acp-bot/`, 스크립트, 옛 로그, 문서 흔적).
3. **설명 + 실행방법 제시**: 봇 설명과 실행 절차 문서화.
4. **PowerShell 창 하나 띄우기**: 새 터미널 실행.
5. **모드 표기 추가**: 답변 시작에 `[Plan]`/`[Build]`(뒤 줄바꿈), 끝에 `[답변종료]` — 일반 대화 답변에만.
6. **restart ps1 생성**: 재시작 스크립트.
7. **ps1 이름 변경**: `run`/`stop`/`restart` 로.
8. **/cmd 기능 추가**: Discord에서 셸 명령 실행 결과 반환, `[Cmd]`…`[종료]` (owner 전용, `/cmd`만).
9. **restart 에 재빌드 포함**: `restart.ps1` 이 항상 재빌드.
10. **전 명령 텍스트 트리거 + 공통 함수**: 모든 명령에 `/`·`!` 트리거, 단일 디스패치 (요청 배경: `/cmd`가 명령으로 안 됨).
11. **응답 공개 범위 = 전부 공개**: 슬래시/텍스트 모두 채널 공개.
12. **스크립트 재빌드 재확인**: restart 재빌드 여부 확인.
13. **`/cmd` 등록/텍스트 감지 확인**: 현재 구조 설명 요청.
14. **세션 회고 문서 작성** (본 문서 이전 버전).
15. **chat.Transport 인터페이스 제거**: `internal/chat` 통째 삭제, bridge 가 discord 직접 사용.
16. **bot-config 에서 read 안 되는 항목 삭제**: `_` 접두 키 제거.
17. **외부 API 의존 테스트 삭제**: OpenCode(`opencode acp`) 기동 테스트 제거(acp-smoke 는 별도 유지로 확정).
18. **`go run .` 으로 통합 + ps1 삭제**: 모듈을 레포 루트로, main 루트 통합, acp-smoke 는 별도.
19. **카테고리 하위 채널 허용**: `morbes_go_db` 무응답 → 카테고리(`coding_bots`) 기준 허용 구현.
20. **첨부 파일 지원 문의**: 결론 "현재 미지원" — 지원 계획 제시(경로 안내 방식 + 이미지 블록 옵션).

### 메모
- 요청 15·18 은 서로 상충되게 진행됨(추상화 도입 → 일부 폐기). 다음엔 "추상화 강도/재사용 범위"를 먼저 확정하는 것이 비용을 줄인다.
- 요청 19 는 "카테고리 자동 허용"을 처음부터 넣었으면 채널별 ID 등록 삽질이 없었다.
