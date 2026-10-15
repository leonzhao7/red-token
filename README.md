# red-token

## Release

Run `./release.sh` to build the frontend and produce standalone Linux and Windows amd64 executables in `backend/dist/`. The build requires Go, Node.js/npm, GCC, and MinGW-w64 (`x86_64-w64-mingw32-gcc`) for the Windows target. Use `./release.sh linux` or `./release.sh windows` to build one target.

Run the generated executable from the directory where you want `red-token.db` to be stored. Like `dev.sh`, it listens for the backend on `http://localhost:4000` and the frontend on `http://localhost:6174`; open the latter to use the UI. Both listeners run in the same executable and use the same embedded frontend and backend. Set `RT_LISTEN_ADDR`, `RT_FRONTEND_LISTEN_ADDR`, or `RT_DB_PATH` to override the defaults.
