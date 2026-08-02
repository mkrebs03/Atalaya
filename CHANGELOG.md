# Changelog

## 0.0.1 (2026-08-02)


### Features

* **agent:** add remote enrollment command ([72ab83f](https://github.com/mkrebs03/Atalaya/commit/72ab83f973d3ce5dbe2e9a17fc94258be9ef420c))
* **alerts:** add alerting and Discord notifications ([90fca9f](https://github.com/mkrebs03/Atalaya/commit/90fca9f3345e069c104548365d5ed87c6a7e0f6b))
* **auth:** complete phase 1 authentication ([3baf19f](https://github.com/mkrebs03/Atalaya/commit/3baf19fa3f4f589c0e3b604b5f8026f600de89eb))
* **checks:** add HTTP and TCP service checks ([48bac11](https://github.com/mkrebs03/Atalaya/commit/48bac11222ec75e48b75b45dc912c43e9c0d613f))
* **cli:** complete Phase 1A bootstrap ([7180a5f](https://github.com/mkrebs03/Atalaya/commit/7180a5fcad85fc48f1ebcc3a7f386290680c45e2))
* **database:** add migrations and administrator persistence ([90ba28b](https://github.com/mkrebs03/Atalaya/commit/90ba28b3d2fe2503e4490c4f5f4a1cf0ad5e9094))
* **hosts:** add enrollment and management API ([6a5a827](https://github.com/mkrebs03/Atalaya/commit/6a5a82765a43ec0454176fa65c44debad9190e2e))
* **install:** support pipe-based installer ([c940b16](https://github.com/mkrebs03/Atalaya/commit/c940b16cb3f104b8df3f7296d2f8477b7db692e5))
* **metrics:** add historical metrics and retention ([952aab9](https://github.com/mkrebs03/Atalaya/commit/952aab9946de247df20f696a127b984370558dfb))
* **phase2a:** add local agent and current-state views ([227c11a](https://github.com/mkrebs03/Atalaya/commit/227c11a487057aa7d518f74b663fec2dc6e53cd8))
* **release:** automate stable and dev channels ([28cfd2b](https://github.com/mkrebs03/Atalaya/commit/28cfd2b61facaff6d47d8cddd7e76198f5ba97e8))
* **security:** add configuration and crypto foundation ([e2bbb50](https://github.com/mkrebs03/Atalaya/commit/e2bbb505e28ed371eaa724cb3f70256f52680bbf))
* **server:** add Phase 0 CLI bootstrap ([a8e42b5](https://github.com/mkrebs03/Atalaya/commit/a8e42b515b2416da106246e49c821acf165fbec3))
* **web:** add host management workspace ([6fe16cf](https://github.com/mkrebs03/Atalaya/commit/6fe16cf22cfb6b5752b6bc16323e00f657c7a8dc))
* **web:** add Phase 0 bootstrap interface ([49db45b](https://github.com/mkrebs03/Atalaya/commit/49db45b0b94ca99162284e3b15fa960b89de46f0))


### Bug Fixes

* **agent:** standardize local agent display name ([41c0778](https://github.com/mkrebs03/Atalaya/commit/41c07781999a6b90797a1e9aff4f822151048393))
* **api:** allow enrollment token pagination lookahead ([a6b9b88](https://github.com/mkrebs03/Atalaya/commit/a6b9b88727e0ba0232f4806ded958649a6f0085f))
* **api:** decode host patch fields ([481becf](https://github.com/mkrebs03/Atalaya/commit/481becf4030ee4ab061d1befc9f98854bd10d8d0))
* **checks:** address review findings ([4515ac2](https://github.com/mkrebs03/Atalaya/commit/4515ac2ebf5692baee877e51efd284a6b515dcbd))
* **ci:** repair upgrade fixture and Linux snapshots ([51dd6c0](https://github.com/mkrebs03/Atalaya/commit/51dd6c0322975c75f1e9ece2de2ac76e638a7fab))
* **ci:** run piped install test on Linux ([8b37951](https://github.com/mkrebs03/Atalaya/commit/8b379511c52236c16d70c319c4f76257c3900e69))
* **ci:** skip host-only systemd verification ([0590d49](https://github.com/mkrebs03/Atalaya/commit/0590d499d450848af0887035cb68ddedb5be1ec1))
* **collection:** classify proxy failures as unreachable ([6865ae4](https://github.com/mkrebs03/Atalaya/commit/6865ae460308113c96a3cb9b2fc6392cfd93c5e9))
* **database:** avoid narrowing notification version ([1337161](https://github.com/mkrebs03/Atalaya/commit/1337161bd83de9253367be1bf0abcd3fc239ecfb))
* **install:** restart services after upgrade ([bde12fa](https://github.com/mkrebs03/Atalaya/commit/bde12fa2df624d44d5512ec1962bd577e04ae0a1))
* **install:** wait for services before healthcheck ([b8651d8](https://github.com/mkrebs03/Atalaya/commit/b8651d8f590764c4704112e1a45076003dc69c29))
* **ops:** address installer and status review findings ([2c80b0b](https://github.com/mkrebs03/Atalaya/commit/2c80b0b76794a7da0e5891bbcd1c6074c2e2f9a0))
* **platform:** support macOS local verification ([64e29c8](https://github.com/mkrebs03/Atalaya/commit/64e29c897df2aabec1fac44da7163226e51b9949))
* **release:** preserve immutable dev history ([fce5150](https://github.com/mkrebs03/Atalaya/commit/fce51509df933dff102cfc578f98e53ebe9144fc))
* **release:** recreate immutable dev release ([aa23255](https://github.com/mkrebs03/Atalaya/commit/aa23255b4934ab21f757e3f5d4bc8e87bb691115))
* **tailscale:** handle Serve route status formats ([a59eb67](https://github.com/mkrebs03/Atalaya/commit/a59eb6727174a29d52d66815598598fcbe027e05))

## Changelog

All notable stable releases of Atalaya will be documented here.
