package com.example;

import org.gradle.api.Plugin;
import org.gradle.api.Project;

public class IncludedPlugin implements Plugin<Project> {
    @Override
    public void apply(Project project) {
        // no-op plugin — exercises the included-build path
    }
}
