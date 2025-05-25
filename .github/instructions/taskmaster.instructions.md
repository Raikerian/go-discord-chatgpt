# Task Master Integration Instructions for GitHub Copilot

## Overview

This project uses **Task Master** for task-driven development workflows. Task Master provides structured project management with AI-powered task generation, breakdown, and tracking capabilities.

## Core Workflow Integration

When working on this project, GitHub Copilot should be aware of the Task Master workflow:

### Starting Development Sessions
- Tasks are managed in `tasks/tasks.json` with individual task files in the `tasks/` directory
- Check current project status by referencing task files or suggesting `get_tasks` MCP tool usage
- Identify next available work using dependency chains and task status
- Consider task complexity and priority when suggesting implementation approaches

### Task-Driven Implementation
- Break down complex features into specific, actionable subtasks
- Follow task dependencies and priority ordering
- Implement code that aligns with task descriptions and requirements
- Update task status as work progresses (pending → in-progress → done)
- Log implementation decisions and progress in task details

### Task Structure Understanding
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

## AI Model Configuration

Task Master uses configurable AI models for different operations:
- **Main Model**: Primary task generation and updates
- **Research Model**: Research-backed operations with enhanced accuracy
- **Fallback Model**: Backup when primary model fails

Configuration is managed through:
- `.taskmasterconfig` file for model selections and parameters
- Environment variables (`.env` or `mcp.json`) for API keys only

## MCP Tools vs CLI Commands

**Prefer MCP tools** when available for better integration:
- MCP tools provide structured data and better error handling
- CLI commands serve as fallback for direct user interaction
- All major operations have both MCP and CLI variants

### Key MCP Tools Reference

#### Project Setup
- `initialize_project`: Set up Task Master structure
- `parse_prd`: Generate initial tasks from requirements document

#### Task Management
- `get_tasks`: List all tasks with status filtering
- `get_task`: View specific task details
- `next_task`: Find next available task based on dependencies
- `add_task`: Create new tasks with AI assistance
- `expand_task`: Break down complex tasks into subtasks
- `set_task_status`: Update task completion status

#### Task Updates
- `update_task`: Modify specific task with new information
- `update_subtask`: Append implementation notes to subtasks
- `update`: Update multiple future tasks based on changes

#### Complexity Analysis
- `analyze_project_complexity`: Evaluate task complexity for breakdown
- `complexity_report`: View formatted complexity analysis

## Development Workflow Guidelines

### Task Selection
1. Check dependencies - all prerequisite tasks must be 'done'
2. Consider priority level (high → medium → low)
3. Follow ID order for tasks of equal priority
4. Use complexity analysis to determine subtask needs

### Implementation Process
1. Review task details and test strategy before coding
2. Implement following project architectural patterns
3. Log progress and decisions in subtask details
4. Verify implementation against test strategy
5. Mark tasks as 'done' when complete and verified

### Handling Implementation Drift
When code implementation differs from planned approach:
- Use `update_task` for single task modifications
- Use `update` for updating multiple dependent tasks
- Document rationale for changes in task details
- Update test strategies if verification approach changes

### Task Breakdown Strategy
- Use complexity analysis to guide subtask generation
- Aim for 3-7 subtasks per complex task
- Each subtask should be implementable in 1-2 hours
- Include implementation details and verification steps
- Maintain clear dependency relationships

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

## AI-Powered Operations

Several Task Master operations use AI processing:
- `parse_prd`: PRD analysis and task generation
- `add_task`: Intelligent task structuring
- `expand_task`: Smart subtask breakdown
- `update_*`: Context-aware task updates
- `analyze_project_complexity`: Complexity evaluation

These operations may take 30-60 seconds to complete and provide detailed, contextually appropriate results.

## Error Handling and Validation

### Dependency Management
- Validate task dependencies before implementation
- Use `validate_dependencies` to check for circular references
- Fix dependency issues with `fix_dependencies`
- Maintain proper parent-child relationships in subtasks

### Task Reorganization
- Use `move_task` to restructure task hierarchy
- Support batch moves for multiple tasks
- Maintain dependency integrity during reorganization
- Regenerate task files after structural changes

## Best Practices for Copilot Integration

1. **Context Awareness**: Consider current task context when suggesting code
2. **Dependency Respect**: Don't suggest implementations that break task dependencies
3. **Incremental Progress**: Suggest changes that align with current task scope
4. **Documentation**: Include task-relevant comments in generated code
5. **Testing**: Suggest test implementations that match task test strategies
6. **Configuration**: Propose config changes when tasks require new settings

## Common Workflow Patterns

### Starting New Features
1. Review related tasks and dependencies
2. Understand implementation requirements from task details
3. Suggest code structure that aligns with task breakdown
4. Consider integration points with existing components

### Debugging and Fixes
1. Identify which tasks the fix relates to
2. Update task details if implementation approach changes
3. Suggest logging or monitoring improvements in task context
4. Consider impact on dependent tasks

### Refactoring
1. Update affected tasks with new architectural decisions
2. Maintain task dependency relationships during refactoring
3. Update test strategies for modified components
4. Document refactoring rationale in task details

This integration ensures GitHub Copilot suggestions align with the project's task-driven development approach and enhance the Task Master workflow.
