# Log integrity demos

Start the local dashboard with `powershell -ExecutionPolicy Bypass -File .\systems\demo-scripts-v2\run-dashboard.ps1`, then open `http://127.0.0.1:4170`. It reads the 100,000-record Spark source and local Docker proof-store counts. Its animated stages and event feed update while the PowerShell workflows run.

From the repository root in PowerShell:

```powershell
.\systems\demo-scripts-v2\verify.ps1
.\systems\demo-scripts-v2\verify.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 `
    -Start '2017-03-01T00:00:00Z' -End '2017-12-31T00:00:00Z' -Repeats 5
.\systems\demo-scripts-v2\verify-blas.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 `
    -Start '2017-03-01T00:00:00Z' -End '2017-12-31T00:00:00Z' -Repeats 5
.\systems\demo-scripts-v2\tamper.ps1
.\systems\demo-scripts-v2\localize.ps1 -Repeats 5
.\systems\demo-scripts-v2\tamper.ps1 -Restore
```

Edit `query.psd1` to change the shared default query. Both `verify.ps1` and `tamper.ps1` accept the query flags shown above. The tenant is required: the ALL locator key is ordered tenant, service, log type, region, time, log ID. Times are RFC 3339. The start is inclusive, the end is exclusive, and `-At` selects the following one-second interval starting at that instant.

## Verification

The scripts use the existing local Docker demo ingest and the 100,000-record Spark source file. HMF-Audit authenticates the query with ALL and prints the matching log IDs. DHT-backed and EVSEB receive those records by their native IDs. BLAS runs its native query, and the demo matches its verified file and entry positions against the ALL result.

The schemes run sequentially. Each gets one warm-up and the same number of timed requests. The HMF-Audit locator lookup and the ID mapping happen outside the timers. BLAS's timed request includes its native query and proof generation. The script shows progress and names the fastest valid run when at least two schemes complete successfully.

## BLAS sourced verification

`verify-blas.ps1` runs the native BLAS attribute query first. If BLAS accepts its query proof, the script maps its verified file positions to source log IDs and publishes that exact batch. HMF-Audit then authenticates its query result and checks it matches the published IDs; DHT-backed and EVSEB verify the corresponding source ordinals. The dashboard's **BLAS source** mode runs this workflow with the selected query.

| Result | Meaning |
|---|---|
| `VALID` | The proof passed for the authenticated result. |
| `REJECTED` | A proof failed or the verified records differ from the ALL result. |
| `ERROR` | Verification could not finish, for example because an ingest or anchor is missing. |

Exit codes are 0 for success, 1 for rejected, and 2 for an error. An authenticated empty result is valid and has no timings.

## Tampering and localization

`tamper.ps1` verifies the clean batch in HMF-Audit and EVSEB, then flips a byte in the first matching log's ciphertext in both stores in a single PostgreSQL transaction. It records the batch and the before-and-after hashes so `-Restore` can restore the ciphertexts.

`localize.ps1` uses the query saved by `tamper.ps1`. It runs HMF-Audit and EVSEB sequentially and measures detection plus localization. It names the faster scheme if both identify exactly the recorded log ID. Run `tamper.ps1 -Restore` after the comparison to restore the original rows.
