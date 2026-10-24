package com.iknalos.warpgo.update

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import androidx.core.content.pm.PackageInfoCompat
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/** What CI publishes next to every APK as latest.json. */
data class UpdateInfo(
    val build: Long,
    val version: String,
    val apk: String,
    val sha256: String,
    val notes: String,
)

/**
 * In-app updates, the same way Drift & Bloom and TwoStep do them: read the newest
 * release's latest.json, and when its build number beats this one, download the
 * APK, check its SHA-256, and hand it to the system installer. Android only
 * installs it over this app when it's signed with the same key.
 */
object Updater {
    const val REPO = "iknalos/WarpGo"
    private const val LATEST_URL = "https://github.com/$REPO/releases/latest/download/latest.json"
    private const val APK_PREFIX = "https://github.com/$REPO/releases/download/"

    fun installedBuild(context: Context): Long =
        PackageInfoCompat.getLongVersionCode(context.packageManager.getPackageInfo(context.packageName, 0))

    /** Blocking. Null when nothing has been released with latest.json yet. */
    @Throws(IOException::class)
    fun check(): UpdateInfo? {
        val conn = open(LATEST_URL)
        try {
            val code = conn.responseCode
            if (code == 404) return null
            if (code != 200) throw IOException("GitHub answered HTTP $code")
            val o = JSONObject(conn.inputStream.bufferedReader().use { it.readText() })
            return UpdateInfo(
                build = o.getLong("build"),
                version = o.optString("version"),
                apk = o.getString("apk"),
                sha256 = o.getString("sha256").lowercase(),
                notes = o.optString("notes"),
            )
        } finally {
            conn.disconnect()
        }
    }

    /** Blocking. Downloads to cache/updates/update.apk and refuses a file whose SHA-256 doesn't match. */
    @Throws(IOException::class)
    fun download(context: Context, info: UpdateInfo, onProgress: (Float) -> Unit): File {
        if (!info.apk.startsWith(APK_PREFIX)) throw IOException("Refusing an update from outside $REPO")
        val dir = File(context.cacheDir, "updates").apply { mkdirs() }
        val out = File(dir, "update.apk")
        val conn = open(info.apk)
        try {
            if (conn.responseCode != 200) throw IOException("Download failed: HTTP ${conn.responseCode}")
            val total = conn.contentLengthLong
            val digest = MessageDigest.getInstance("SHA-256")
            conn.inputStream.use { inp ->
                out.outputStream().use { o ->
                    val buf = ByteArray(64 * 1024)
                    var got = 0L
                    var last = 0L
                    while (true) {
                        val n = inp.read(buf)
                        if (n < 0) break
                        o.write(buf, 0, n)
                        digest.update(buf, 0, n)
                        got += n
                        val now = System.currentTimeMillis()
                        if (now - last > 120) {
                            last = now
                            onProgress(if (total > 0) got.toFloat() / total else -1f)
                        }
                    }
                }
            }
            val sha = digest.digest().joinToString("") { "%02x".format(it) }
            if (sha != info.sha256) {
                out.delete()
                throw IOException("The download was damaged (checksum mismatch). Try again.")
            }
            onProgress(1f)
            return out
        } finally {
            conn.disconnect()
        }
    }

    fun canInstall(context: Context) =
        Build.VERSION.SDK_INT < 26 || context.packageManager.canRequestPackageInstalls()

    /** First time only: Android's "allow installs from this app" switch. */
    fun openInstallPermission(context: Context) {
        context.startActivity(
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        )
    }

    fun install(context: Context, apk: File) {
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.updates", apk)
        context.startActivity(
            Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        )
    }

    private fun open(url: String): HttpURLConnection =
        (URL(url).openConnection() as HttpURLConnection).apply { // follows GitHub's https redirects
            connectTimeout = 15_000
            readTimeout = 30_000
            useCaches = false
            setRequestProperty("Cache-Control", "no-cache")
            setRequestProperty("User-Agent", "WarpGo-Android")
        }
}
