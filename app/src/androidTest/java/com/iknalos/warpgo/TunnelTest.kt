package com.iknalos.warpgo

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.os.ParcelFileDescriptor
import android.widget.Button
import android.widget.TextView
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TestName
import org.junit.runner.RunWith
import java.io.File

/**
 * Runs on the CI emulator, whose network blocks nothing: every option must pass
 * real data there, and both tunnel types must carry the device's own traffic.
 * A release is only published when these pass, so WireGuard can't silently break.
 * ci/device-test.sh grants VPN permission first (appops ACTIVATE_VPN).
 */
@RunWith(AndroidJUnit4::class)
class TunnelTest {

    @get:Rule
    val name = TestName()

    private val inst = InstrumentationRegistry.getInstrumentation()
    private val ctx: Context = inst.targetContext
    private val out = File(ctx.filesDir, "ci").apply { mkdirs() }

    @Before
    fun freshSettings() {
        ctx.getSharedPreferences("ui", Context.MODE_PRIVATE).edit().clear().commit()
        ctx.getSharedPreferences("auto_memory", Context.MODE_PRIVATE).edit().clear().commit()
    }

    @After
    fun tearDown() {
        WarpManager.stats().takeIf { it.running }?.let {
            com.iknalos.warpgo.warpcore.Warpcore.stop()
            ctx.stopService(Intent(ctx, WarpVpnService::class.java))
        }
        waitFor(5_000, "the tunnel to stop") { !WarpManager.stats().running }
        File(out, "${name.methodName}-log.txt").writeText(WarpManager.logs())
    }

    @Test
    fun everyOptionPassesData() {
        val report = StringBuilder()
        val failed = mutableListOf<Mode>()
        for (m in Mode.entries) {
            val r = WarpManager.probe(ctx, m)
            report.appendLine("${if (r.ok) "OK  " else "FAIL"}  ${m.label}: ${r.ms} ms, ${r.detail}")
            if (!r.ok) failed += m
        }
        File(out, "probes.txt").writeText(report.toString())
        assertTrue("Every option must pass on an open network:\n$report", failed.isEmpty())
    }

    @Test
    fun wireGuardCarriesTheDevicesTraffic() = carriesTraffic(Mode.WG_4500)

    @Test
    fun masqueCarriesTheDevicesTraffic() = carriesTraffic(Mode.MASQUE_H3)

    @Test
    fun autoConnectsFromTheScreen() {
        ActivityScenario.launch(MainActivity::class.java).use { sc ->
            sc.onActivity { it.findViewById<Button>(R.id.toggleButton).performClick() }
            waitFor(60_000, "Auto to connect") { WarpManager.stats().running }
            shell("ping -c 3 -W 3 one.one.one.one")
            waitFor(15_000, "data through the tunnel") { WarpManager.stats().rx > 0 }
            Thread.sleep(1_500) // the screen refreshes once a second
            screenshot("auto-connected")
            val status = textOf(sc, R.id.statusText)
            assertTrue("status after Auto: $status", status.startsWith("Connected"))
            sc.onActivity { it.findViewById<Button>(R.id.toggleButton).performClick() }
            waitFor(10_000, "Disconnect") { !WarpManager.stats().running }
        }
    }

    @Test
    fun testEveryOptionScreen() {
        ActivityScenario.launch(MainActivity::class.java).use { sc ->
            sc.onActivity { it.findViewById<Button>(R.id.testAllButton).performClick() }
            waitFor(90_000, "all tests to finish") {
                textOf(sc, R.id.resultsText).lines().count { it.startsWith("✓") || it.startsWith("✗") } == Mode.entries.size
            }
            Thread.sleep(500)
            screenshot("test-every-option")
        }
    }

    private fun carriesTraffic(mode: Mode) {
        // The app must be in the foreground for startService.
        ActivityScenario.launch(MainActivity::class.java).use {
            WarpManager.connect(ctx, mode)
            waitFor(20_000, "${mode.label} to start") { WarpManager.stats().running }
            // Shell traffic is not excluded from the VPN, unlike WarpGo's own.
            val ping = shell("ping -c 5 -W 3 one.one.one.one")
            File(out, "${mode.id}-ping.txt").writeText(ping)
            val s = WarpManager.stats()
            assertTrue("nothing came back through ${mode.label}: $s\n$ping", s.rx > 0 && s.tx > 0)
            assertTrue("ping got no replies through ${mode.label}:\n$ping", ping.contains("bytes from"))
            assertTrue("WireGuard handshake / MASQUE session not up: $s", s.connected)
        }
    }

    private fun shell(cmd: String): String {
        val pfd = inst.uiAutomation.executeShellCommand(cmd)
        return ParcelFileDescriptor.AutoCloseInputStream(pfd).bufferedReader().use { it.readText() }
    }

    private fun screenshot(file: String) {
        val bmp = inst.uiAutomation.takeScreenshot() ?: return
        File(out, "$file.png").outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun textOf(sc: ActivityScenario<MainActivity>, id: Int): String {
        var text = ""
        sc.onActivity { text = it.findViewById<TextView>(id).text.toString() }
        return text
    }

    private fun waitFor(ms: Long, what: String, cond: () -> Boolean) {
        val end = System.currentTimeMillis() + ms
        while (System.currentTimeMillis() < end) {
            if (cond()) return
            Thread.sleep(200)
        }
        throw AssertionError("timed out waiting for $what\n--- engine log ---\n${WarpManager.logs().takeLast(4000)}")
    }
}
