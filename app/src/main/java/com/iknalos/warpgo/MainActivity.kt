package com.iknalos.warpgo

import android.content.ClipData
import android.content.ClipboardManager
import android.graphics.Typeface
import android.net.VpnService
import android.os.Bundle
import android.view.View
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.iknalos.warpgo.databinding.ActivityMainBinding
import com.iknalos.warpgo.update.UpdateInfo
import com.iknalos.warpgo.update.Updater
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.io.IOException

class MainActivity : AppCompatActivity() {

    private enum class Tone(val color: Int) {
        IDLE(0xFF757575.toInt()),
        GOOD(0xFF2E7D32.toInt()),
        WARN(0xFFE65100.toInt()),
        ERROR(0xFFC62828.toInt()),
    }

    private lateinit var binding: ActivityMainBinding
    private var busy = false
    private var wasRunning = false
    private var afterVpnPermission: (() -> Unit)? = null
    private var poller: Job? = null
    private var available: UpdateInfo? = null
    private var pendingApk: File? = null
    private val ui by lazy { getSharedPreferences("ui", MODE_PRIVATE) }

    private val vpnPermission =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            val action = afterVpnPermission
            afterVpnPermission = null
            if (result.resultCode == RESULT_OK && action != null) action()
            else setStatus("VPN permission denied", Tone.ERROR)
        }

    private val modeButtons by lazy {
        mapOf(
            binding.port4500.id to Mode.WG_4500,
            binding.port2408.id to Mode.WG_2408,
            binding.port500.id to Mode.WG_500,
            binding.masqueH3.id to Mode.MASQUE_H3,
            binding.masqueH2.id to Mode.MASQUE_H2,
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)

        restoreSelection()
        binding.portGroup.setOnCheckedChangeListener { _, _ ->
            ui.edit().putString("mode", selectedMode()?.id ?: "auto").apply()
        }
        binding.toggleButton.setOnClickListener { onToggle() }
        binding.testAllButton.setOnClickListener { if (!busy) runTests(connect = false) }
        binding.resetButton.setOnClickListener {
            if (WarpManager.stats().running) {
                toast("Disconnect first")
            } else {
                WarpManager.resetRegistration(this)
                setStatus("Accounts cleared. The next connect sets them up again.", Tone.IDLE)
            }
        }
        binding.showLogButton.setOnClickListener { showLog() }
        binding.checkUpdatesButton.setOnClickListener { checkForUpdate(manual = true) }
        binding.updateNow.setOnClickListener { startUpdate() }
        binding.updateLater.setOnClickListener { binding.updateBanner.visibility = View.GONE }
        binding.buildText.text = "Build ${Updater.installedBuild(this)}"

        checkForUpdate(manual = false)
    }

    override fun onResume() {
        super.onResume()
        poller?.cancel()
        poller = lifecycleScope.launch {
            while (isActive) {
                refreshState()
                delay(1000)
            }
        }
        pendingApk?.let {
            if (Updater.canInstall(this)) {
                pendingApk = null
                Updater.install(this, it)
            }
        }
    }

    override fun onPause() {
        poller?.cancel()
        super.onPause()
    }

    /** The chosen option, or null for Auto. */
    private fun selectedMode(): Mode? = modeButtons[binding.portGroup.checkedRadioButtonId]

    private fun restoreSelection() {
        val saved = Mode.fromId(ui.getString("mode", "auto"))
        val button = modeButtons.entries.firstOrNull { it.value == saved }?.key ?: binding.modeAuto.id
        binding.portGroup.check(button)
    }

    private fun onToggle() {
        if (busy) return
        if (WarpManager.stats().running) {
            WarpManager.disconnect(this)
            return
        }
        withVpnPermission {
            val mode = selectedMode()
            if (mode == null) runTests(connect = true) else startTunnel(mode)
        }
    }

    private fun withVpnPermission(action: () -> Unit) {
        val intent = VpnService.prepare(this)
        if (intent == null) {
            action()
        } else {
            afterVpnPermission = action
            vpnPermission.launch(intent)
        }
    }

    private fun startTunnel(mode: Mode) {
        WarpManager.connect(this, mode)
        setStatus("Connecting · ${mode.label}…", Tone.IDLE)
    }

    /**
     * Tests the options one at a time. Auto (connect = true) stops at the first
     * one that passes data and connects with it; "Test every option" runs them all.
     */
    private fun runTests(connect: Boolean) {
        lifecycleScope.launch {
            setBusy(true)
            if (WarpManager.stats().running) {
                WarpManager.disconnect(this@MainActivity)
                withContext(Dispatchers.IO) { waitUntilStopped() }
            }
            val key = WarpManager.networkKey(this@MainActivity)
            val order = if (connect) {
                Mode.autoOrder(WarpManager.remembered(this@MainActivity, key))
            } else {
                Mode.entries.toList()
            }
            val lines = mutableListOf<String>()
            var winner: Mode? = null
            for (m in order) {
                setStatus("Testing ${m.label}…", Tone.IDLE)
                showResults(lines + "…  ${m.label}")
                val r = withContext(Dispatchers.IO) { WarpManager.probe(applicationContext, m) }
                lines += if (r.ok) "✓  ${m.label} (${r.ms} ms)" else "✗  ${m.label}: ${r.detail}"
                showResults(lines)
                if (r.ok && winner == null) {
                    winner = m
                    if (connect) break
                }
            }
            setBusy(false)
            val found = winner
            if (found == null) {
                setStatus("Nothing got through on this network", Tone.ERROR)
                return@launch
            }
            WarpManager.remember(this@MainActivity, key, found)
            if (connect) startTunnel(found) else setStatus("${found.label} works here", Tone.GOOD)
        }
    }

    private fun waitUntilStopped() {
        repeat(30) {
            if (!WarpManager.stats().running) return
            Thread.sleep(100)
        }
    }

    private fun refreshState() {
        val s = WarpManager.stats()
        binding.toggleButton.text = if (s.running) "Disconnect" else "Connect"
        binding.statusDetail.visibility = if (s.running) View.VISIBLE else View.GONE
        if (busy) return
        if (!s.running) {
            val error = WarpManager.lastError
            when {
                error != null -> setStatus("Failed: $error", Tone.ERROR)
                wasRunning -> setStatus("Disconnected", Tone.IDLE)
            }
            wasRunning = false
            return
        }
        wasRunning = true
        val label = s.mode?.label ?: "tunnel"
        binding.statusDetail.text = "↓ ${bytes(s.rx)}    ↑ ${bytes(s.tx)}"
        when {
            // rx counts what came back through the tunnel, so this is real data, not just "up".
            s.rx > 0 -> setStatus("Connected ✓ · $label", Tone.GOOD)
            s.seconds >= 10 && s.tx > 0 ->
                setStatus("$label is up, but no data is coming back. Try Auto.", Tone.WARN)
            else -> setStatus("Connecting · $label…", Tone.IDLE)
        }
    }

    private fun showResults(lines: List<String>) {
        binding.resultsText.visibility = View.VISIBLE
        binding.resultsText.text = lines.joinToString("\n")
    }

    private fun setBusy(value: Boolean) {
        busy = value
        binding.toggleButton.isEnabled = !value
        binding.testAllButton.isEnabled = !value
        binding.progress.visibility = if (value) View.VISIBLE else View.GONE
    }

    private fun setStatus(text: String, tone: Tone) {
        binding.statusText.text = text
        binding.statusText.setTextColor(tone.color)
    }

    private fun showLog() {
        val log = WarpManager.logs().ifBlank { "(empty)" }
        val view = TextView(this).apply {
            text = log
            setTextIsSelectable(true)
            typeface = Typeface.MONOSPACE
            textSize = 11f
            setPadding(32, 16, 32, 16)
        }
        AlertDialog.Builder(this)
            .setTitle("Engine log")
            .setView(ScrollView(this).apply { addView(view) })
            .setNeutralButton("Copy") { _, _ ->
                getSystemService(ClipboardManager::class.java)
                    .setPrimaryClip(ClipData.newPlainText("WarpGo log", log))
                toast("Log copied")
            }
            .setPositiveButton("Close", null)
            .show()
    }

    // ---- updates --------------------------------------------------------------

    private fun checkForUpdate(manual: Boolean) {
        lifecycleScope.launch {
            val info = try {
                withContext(Dispatchers.IO) { Updater.check() }
            } catch (e: IOException) {
                if (manual) toast("Couldn't check for updates: ${e.message}")
                return@launch
            }
            if (info != null && info.build > Updater.installedBuild(this@MainActivity)) {
                available = info
                binding.updateText.text = buildString {
                    append("WarpGo ${info.version} is available.")
                    if (info.notes.isNotBlank()) append("\n${info.notes}")
                }
                binding.updateBanner.visibility = View.VISIBLE
            } else if (manual) {
                toast("WarpGo is up to date")
            }
        }
    }

    private fun startUpdate() {
        val info = available ?: return
        binding.updateNow.isEnabled = false
        binding.updateProgress.visibility = View.VISIBLE
        binding.updateProgress.isIndeterminate = true
        lifecycleScope.launch {
            try {
                val apk = withContext(Dispatchers.IO) {
                    Updater.download(applicationContext, info) { p ->
                        runOnUiThread {
                            binding.updateProgress.isIndeterminate = p < 0
                            if (p >= 0) binding.updateProgress.progress = (p * 1000).toInt()
                        }
                    }
                }
                if (Updater.canInstall(this@MainActivity)) {
                    Updater.install(this@MainActivity, apk)
                } else {
                    pendingApk = apk
                    toast("Allow WarpGo to install updates, then come back")
                    Updater.openInstallPermission(this@MainActivity)
                }
            } catch (e: IOException) {
                binding.updateText.text = "Update failed: ${e.message}"
            } finally {
                binding.updateNow.isEnabled = true
                binding.updateProgress.visibility = View.GONE
            }
        }
    }

    private fun toast(text: String) = Toast.makeText(this, text, Toast.LENGTH_LONG).show()

    private fun bytes(n: Long): String = when {
        n >= 1L shl 30 -> "%.1f GB".format(n / (1L shl 30).toDouble())
        n >= 1L shl 20 -> "%.1f MB".format(n / (1L shl 20).toDouble())
        n >= 1L shl 10 -> "%.0f KB".format(n / (1L shl 10).toDouble())
        else -> "$n B"
    }
}
