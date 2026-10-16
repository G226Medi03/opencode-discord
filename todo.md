# TODO — `chat-app-adapter` 라이브러리 분리

> 결과: 라이브러리 신규 레포 생성·배포 완료, 앱 재배선 완료(빌드/테스트 통과).
> 앱 커밋은 보류 (사용자 WIP "강제 모델" 기능과 변경이 얽혀 있음).

- 앱: `D:\Coding\opencode_discord` → `G226Medi03/opencode-discord`
- 라이브러리: `G226Medi03/chat-app-adapter` (Public) → `D:\Coding\chat-app-adapter` · tag `v0.1.0`
- Go 1.25.5 / discordgo v0.29.0

## Phase 0 — 준비
- [x] 라이브러리 레포 클론
- [x] 앱 빌드/테스트 기준선 (build OK / 전 테스트 통과)

## Phase 1 — 스캐폴드
- [x] `go.mod` (`github.com/G226Medi03/chat-app-adapter`, go 1.25.5)
- [x] `README.md` / `LICENSE` (MIT) / `.gitattributes`

## Phase 2 — 코어 인터페이스 중립화
- [x] `chat/types.go` (Message, Input, Channel, Button, Style, Choice, MessageRef, CommandSpec…)
- [x] `chat/app.go` — `ChatApp` → `App`
- [x] `chat/logger.go` — `Logger` 인터페이스 (nil 허용)

## Phase 3 — Discord 어댑터
- [x] `chat/discord/client.go`
- [x] `logx` 의존 제거 → `chat.Logger`
- [x] `DiscordClient`→`Client`, `DiscordClientOption`→`Options`
- [x] 공개 API 정리: `BotID`, `OnMessage`, `OnCommand`, `Send`, `Reply`, `SendButtons`

## Phase 4 — chatcmd / chunk
- [x] `chatcmd/` (+test) — `chat.CommandSpec`
- [x] `chunk/` (+test)

## Phase 5 — 검증·배포
- [x] gofmt / vet / build / test 통과
- [x] push main, tag `v0.1.0`

## Phase 6 — 앱 재배선
- [x] `go.mod`: `require github.com/G226Medi03/chat-app-adapter v0.1.0` (replace 없이 공개 태그 사용)
- [x] `internal/discord`, `internal/chatcmd`, `internal/chunk` 삭제
- [x] `internal/bridge/*`: `discord.*`→`chat.*`, 메서드명, `chunk` import, 필드 `discord_client`→`app`
- [x] `main.go`: `discord.New(discord.Options{...})`

## Phase 7 — 검증
- [x] 앱 build / vet / test 통과 (go test ./... 전체 green)
- [x] 스모크: `go run . -version`, `go run . -print-config` 정상

## 남은 것
- [ ] 앱 레포 커밋 (WIP "강제 모델" 변경과 분리 방법 결정 필요)
- [ ] (선택) 라이브러리 내부 정리: `discord_go` 필드, `self` 리시버 → 관용적 이름
- [ ] (선택) 동시 개발 시 `go mod edit -replace=github.com/G226Medi03/chat-app-adapter=../chat-app-adapter`
