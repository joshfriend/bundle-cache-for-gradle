plugins {
    `java-gradle-plugin`
}

gradlePlugin {
    plugins {
        create("included") {
            id = "com.example.included"
            implementationClass = "com.example.IncludedPlugin"
        }
    }
}
