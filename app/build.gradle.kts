plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// CI passes the run number so every build outranks the last one: the in-app
// updater installs anything with a higher versionCode.
val ciVersionCode = System.getenv("VERSION_CODE")?.toIntOrNull() ?: 1
val ciVersionName = System.getenv("VERSION_NAME") ?: "1.0.$ciVersionCode"
val ksPath: String? = System.getenv("SIGNING_KEYSTORE")
val ksPass: String? = System.getenv("SIGNING_PASSWORD")

android {
    namespace = "com.iknalos.warpgo"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.iknalos.warpgo"
        minSdk = 24
        targetSdk = 35
        versionCode = ciVersionCode
        versionName = ciVersionName
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // Pre-registered WARP accounts injected from CI secrets (empty for local
        // builds, in which case the app registers its own accounts at runtime).
        buildConfigField("String", "WARP_PRIVATE_KEY", "\"${System.getenv("WARP_PRIVATE_KEY") ?: ""}\"")
        buildConfigField("String", "WARP_ADDRESS_V6", "\"${System.getenv("WARP_ADDRESS_V6") ?: ""}\"")
        // base64 of the MASQUE account JSON
        buildConfigField("String", "WARP_MASQUE_CONFIG", "\"${System.getenv("WARP_MASQUE_CONFIG") ?: ""}\"")
    }

    signingConfigs {
        create("release") {
            if (ksPath != null) {
                storeFile = file(ksPath)
                storeType = "pkcs12"
                storePassword = ksPass
                keyAlias = "warpgo"
                keyPassword = ksPass
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName(if (ksPath != null) "release" else "debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        viewBinding = true
        buildConfig = true
    }
    lint {
        abortOnError = false
    }
}

dependencies {
    // The tunnel engine (core/warpcore), built by `gomobile bind` in CI.
    implementation(files("libs/warpcore.aar"))

    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    androidTestImplementation("androidx.test:core:1.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
