# BranchBase

> **Zero-config, Git-native local database branching for PostgreSQL, MySQL, SQLite, and MongoDB.**  
> Stop dropping your local database every time you switch Git branches.

---

<p align="center">
  <img src="assets/terminal-demo.svg" alt="BranchBase Terminal Demo" width="800">
</p>

---

## Problem Context: Git vs. Local Database Friction

Every developer working with Docker, local PostgreSQL, MySQL, or SQLite has suffered this loop:

```text
1. You work on `feature/checkout-v2`.
   └── Ran migrations: added table `stripe_orders`, added column `users.billing_tier NOT NULL`.
2. Urgent production bug alert! You run:
   └── `git checkout main`
3. You start the app or run tests on `main`.
4. CRASH:
   └── ActiveRecord::PendingMigrationError / PrismaClientKnownRequestError:
       "column users.billing_tier does not exist" or "schema mismatch detected".
```

### How developers waste hours today:
- **The Nuclear Option:** `docker compose down -v && docker compose up -d`  
  *(Loses all test seeds, logins, and mocked state. Takes minutes to re-seed).*
- **The Manual Rollback Dance:** Trying to rollback migrations on `feature/checkout-v2` before switching, only to lose experimental test data.
- **The `.env` Nightmare:** Manually maintaining `DATABASE_URL_DEV`, `DATABASE_URL_CHECKOUT`, and editing `.env` on every branch change.
- **Cloud Branching (Neon, PlanetScale):** Great developer experience, but proprietary, paid, and requires an internet connection. It doesn't work for offline development or standard local Docker setups.

---

## The Solution: BranchBase

**BranchBase** pairs automated Git branch transitions with database cloning mechanisms and a transparent wire-protocol TCP proxy, keeping your connection strings stable while routing queries to branch-specific databases.

```
                           +---------------------------+
                           |     Developer Machine     |
                           +---------------------------+
                                         |
                                `git checkout branch-b`
                                         |
                                         v
                            [ BranchBase Git Hook ]
                                         |
                    +--------------------+--------------------+
                    |                                         |
          (Detects new branch)                      (Branch Provisioning)
                    |                                         |
                    v                                         v
+---------------------------------------+   +------------------------------------+
|   BranchBase Proxy (Postgres / MySQL) |   |          Local Database            |
+---------------------------------------+   +------------------------------------+
| - App connection string stays stable  |   | - `db_project_main` (frozen)       |
| - Automatically routes queries to the |   | - `db_project_branch_b` (active)   |
|   active Git branch database!         |   |   (Created via engine driver)      |
+---------------------------------------+   +------------------------------------+
```

### Key Highlights
- **Engine-Native Branching:** Creates isolated branch databases using PostgreSQL `CREATE DATABASE ... TEMPLATE`, MySQL schema/data replication, SQLite filesystem Copy-on-Write (`clonefile` / `FICLONE`), or MongoDB aggregation pipelines.
- **Transparent Wire Proxy:** Your application connects to a stable local port (`5432` for PostgreSQL or `3306` for MySQL). The proxy inspects the active Git branch from `.git/HEAD` and routes connections to that branch's database without touching `.env`.
- **Automated Git Hook:** Hooks into `post-checkout` and `post-merge` via lightweight POSIX scripts that run asynchronously.
- **Automatic Cleanup (`prune`):** When you delete or merge a Git branch, `branchbase prune` safely tears down the associated ephemeral database.
- **100% Local & Offline:** No cloud telemetry, no subscription fees, and no internet connection required.

---

## Engine Support & Mechanism Matrix

| Engine | Branching Mechanism | Proxy Routed | Fast Snapshot Requirements |
| :--- | :--- | :---: | :--- |
| **PostgreSQL** | `CREATE DATABASE ... TEMPLATE` | Yes (`5432`) | Terminates active template connections |
| **MySQL / MariaDB** | Table-by-table schema & data copy (`LIKE` + `SELECT`) | Yes (`3306`) | Cleartext proxy mode rejects `ClientSSL` (fail-closed) |
| **SQLite** | Filesystem CoW / Reflink (`clonefile` / `FICLONE`) | No (CLI/Hook) | Requires WAL checkpointing; fallback to copy on NTFS/ext4 |
| **MongoDB** | Aggregation pipeline (`$out`) + index recreation | No (CLI/Hook) | Standard collections supported |
