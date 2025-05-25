# Task Master Integration Instructions for GitHub Copilot

## Overview

This project uses **Task Master** for task-driven development workflows. Task Master provides structured project management with AI-powered task generation, breakdown, and tracking capabilities.

## Task Structure Understanding

When suggesting code or implementations, consider the Task Master task structure:

```yaml
id: 1                          # Unique identifier
title: "Brief description"     # Descriptive title
description: "Summary"         # Concise overview
status: "pending|done|..."     # Current state
dependencies: [2, 3.1]         # Prerequisite task IDs
priority: "high|medium|low"    # Importance level
details: "Implementation..."   # Detailed instructions
testStrategy: "Verification..." # Testing approach
subtasks: [...]                # Nested subtasks
```

## Configuration Integration

Task Master configuration integrates with the project through:
- **`.taskmasterconfig`**: AI models and operational parameters
- **Environment variables**: API keys for AI services (`.env` for CLI, `mcp.json` for MCP)
- **`tasks/`** directory: Task definitions and individual task files

*For setup details, see the main instructions document.*

## Integration with Project Architecture

### Uber Fx Dependency Injection
- Tasks should align with Fx component structure
- Consider service dependencies when planning task order
- Update tasks when component interfaces change

### Configuration Management
- Task implementations should use `config.yaml` patterns
- New features may require configuration additions
- Update config-related tasks when adding new settings

### Testing Integration
- Follow testify patterns established in the project
- Use mockery for interface mocking when needed
- Align test strategies with existing test structure
- Update test tasks when implementation patterns change

## Advanced Workflow Patterns

### Task-Driven Implementation Guidelines
- Break down complex features into specific, actionable subtasks
- Follow task dependencies and priority ordering
- Implement code that aligns with task descriptions and requirements
- Log implementation decisions and progress in task details

### Handling Implementation Changes
When code implementation differs from planned approach:
- Use `update_task` for single task modifications
- Use `update` for updating multiple dependent tasks
- Document rationale for changes in task details
- Update test strategies if verification approach changes

### Error Handling and Validation
- Validate task dependencies before implementation
- Use `validate_dependencies` to check for circular references
- Fix dependency issues with `fix_dependencies`
- Maintain proper parent-child relationships in subtasks

## Best Practices for Copilot Integration

1. **Context Awareness**: Consider current task context when suggesting code
2. **Dependency Respect**: Don't suggest implementations that break task dependencies
3. **Incremental Progress**: Suggest changes that align with current task scope
4. **Documentation**: Include task-relevant comments in generated code
5. **Testing**: Suggest test implementations that match task test strategies
6. **Configuration**: Propose config changes when tasks require new settings
4. Document refactoring rationale in task details

This integration ensures GitHub Copilot suggestions align with the project's task-driven development approach and enhance the Task Master workflow.
