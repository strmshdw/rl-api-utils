const os = require("os");
const readline = require("readline");
const SteamUser = require("steam-user");

const ROCKET_LEAGUE_APP_ID = 252950;

function resolveMachineName() {
    try {
        const host = os.hostname();
        if (host && host.trim()) {
            return host.trim();
        }
    } catch (_) {
        // Fallback to environment identifiers below
    }
    return process.env.COMPUTERNAME || process.env.USERNAME || "rl-sync";
}

// Parse CLI flags and explicit STEAM_* environment variables.
// Never fall back to process.env.USERNAME for the Steam account name because
// Windows always populates USERNAME with the local OS user name.
function parseArgs() {
    const args = process.argv.slice(2);
    const options = {
        username: process.env.STEAM_USERNAME || "",
        password: process.env.STEAM_PASSWORD || "",
        loginKey: process.env.STEAM_LOGIN_KEY || "",
        accountType: process.env.STEAM_ACCOUNT_TYPE || "Primary Account",
        machineName: resolveMachineName()
    };

    for (let i = 0; i < args.length; i++) {
        const arg = args[i];
        if (arg === "--username" && i + 1 < args.length) {
            options.username = args[++i];
        } else if (arg === "--password" && i + 1 < args.length) {
            options.password = args[++i];
        } else if (arg === "--login-key" && i + 1 < args.length) {
            options.loginKey = args[++i];
        } else if (arg === "--account-type" && i + 1 < args.length) {
            options.accountType = args[++i];
        }
    }
    return options;
}

let finished = false;
function finish(exitCode, payload, cleanup) {
    if (finished) {
        return;
    }
    finished = true;
    const output = JSON.stringify(payload) + "\n";
    process.stdout.write(output, () => {
        if (cleanup) {
            try {
                cleanup();
            } catch (_) {
                // Ignore cleanup errors on shutdown
            }
        }
        process.exit(exitCode);
    });
}

function isJwtToken(token) {
    return typeof token === "string" && token.split(".").length === 3;
}

const opts = parseArgs();

if (!opts.username) {
    finish(1, {
        success: false,
        error: "Steam username is required (--username or STEAM_USERNAME)"
    });
} else if (!opts.password && !opts.loginKey) {
    finish(1, {
        success: false,
        error: "Steam password or loginKey is required"
    });
} else {
    runSteamAuth(opts);
}

function runSteamAuth(opts) {
    const user = new SteamUser();
    let savedLoginKey = opts.loginKey || "";
    let fellBackToPassword = false;

    const logOnWithPassword = () => {
        user.logOn({
            accountName: opts.username,
            password: opts.password,
            machineName: opts.machineName
        });
    };

    const handleAuthFailure = (err) => {
        const message = err && err.message ? err.message : "Steam login failed";
        if (opts.loginKey && opts.password && !fellBackToPassword) {
            const invalidKey = !isJwtToken(opts.loginKey) ||
                (err && (err.eresult === SteamUser.EResult.InvalidPassword ||
                    err.eresult === SteamUser.EResult.AccessDenied ||
                    err.eresult === SteamUser.EResult.Expired));
            if (invalidKey) {
                fellBackToPassword = true;
                opts.loginKey = "";
                process.stderr.write(`[${opts.accountType}] Stored Steam login key expired; falling back to password login...\n`);
                logOnWithPassword();
                return;
            }
        }
        finish(1, {
            success: false,
            error: message
        });
    };

    process.on("unhandledRejection", (reason) => {
        const err = reason instanceof Error ? reason : new Error(String(reason || "Steam login failed"));
        handleAuthFailure(err);
    });

    process.on("uncaughtException", (err) => {
        finish(1, {
            success: false,
            error: err && err.message ? err.message : "Unexpected Steam authenticator error"
        });
    });

    user.on("loginKey", (key) => {
        if (key) {
            savedLoginKey = key;
        }
    });

    user.on("refreshToken", (token) => {
        if (token) {
            savedLoginKey = token;
        }
    });

    user.on("loggedOn", async () => {
        try {
            const session = await user.createAuthSessionTicket(ROCKET_LEAGUE_APP_ID);
            const ticket = Buffer.from(session.sessionTicket).toString("hex").toUpperCase();
            const steamID = user.steamID.getSteamID64();

            finish(0, {
                success: true,
                sessionTicket: ticket,
                steamID64: steamID,
                loginKey: savedLoginKey,
                accountName: opts.username
            }, () => user.logOff());
        } catch (error) {
            finish(1, {
                success: false,
                error: "Failed to create auth session ticket: " + (error && error.message ? error.message : String(error))
            }, () => user.logOff());
        }
    });

    user.on("error", handleAuthFailure);

    user.on("steamGuard", (domain, callback) => {
        const prefix = `[${opts.accountType}]`;
        const promptMsg = domain
            ? `${prefix} Steam Guard code needed. Enter code from email (${domain}): `
            : `${prefix} Steam Guard code needed. Enter code from mobile app: `;

        process.stderr.write(promptMsg);

        const rl = readline.createInterface({
            input: process.stdin,
            output: process.stderr
        });

        rl.question("", (code) => {
            rl.close();
            callback(code.trim());
        });
    });

    // Initiate logon
    if (opts.loginKey) {
        if (isJwtToken(opts.loginKey)) {
            user.logOn({
                refreshToken: opts.loginKey,
                machineName: opts.machineName
            });
        } else if (opts.password) {
            fellBackToPassword = true;
            opts.loginKey = "";
            logOnWithPassword();
        } else {
            finish(1, {
                success: false,
                error: "Stored Steam login key is invalid; password is required"
            });
        }
    } else {
        logOnWithPassword();
    }
}
