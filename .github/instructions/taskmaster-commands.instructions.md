# Task Master Command Reference for GitHub Copilot

This document provides comprehensive command reference for Task Master MCP tools and CLI commands.

## Command Categories Overview

Task Master commands are organized into logical categories for different aspects of project management. Each command has both MCP tool and CLI variants - prefer MCP tools for better Copilot integration.

### Initialization & Setup

#### Initialize Project
- **Purpose**: Set up Task Master structure for new projects
- **MCP Tool**: `initialize_project`
- **CLI**: `task-master init`
- **Key Parameters**: `projectRoot`, `yes` (skip prompts)
- **Usage Context**: Project bootstrap, first-time setup
- **Next Steps**: Create PRD and run `parse_prd`

#### Parse Requirements Document  
- **Purpose**: Generate initial tasks from project requirements
- **MCP Tool**: `parse_prd`
- **CLI**: `task-master parse-prd`
- **Key Parameters**: `input` (PRD file path), `force` (overwrite), `research` (AI-enhanced)
- **Usage Context**: Project initialization, requirement changes
- **Important**: AI operation taking 30-60 seconds
- **Template**: Use `scripts/example_prd.txt` as reference

### Model Configuration

#### AI Model Management
- **Purpose**: Configure AI models for different operations
- **MCP Tool**: `models`
- **CLI**: `task-master models`
- **Key Parameters**: `setMain`, `setResearch`, `setFallback`, `listAvailableModels`
- **Usage Context**: Setting up AI capabilities, changing model preferences
- **Configuration**: Stored in `.taskmasterconfig`
- **API Keys**: Required in `.env` (CLI) or `mcp.json` (MCP)

### Task Listing & Discovery

#### List All Tasks
- **Purpose**: Overview of project status and task inventory
- **MCP Tool**: `get_tasks`
- **CLI**: `task-master list`
- **Key Parameters**: `status` (filter), `withSubtasks` (include nested)
- **Usage Context**: Session start, progress review, status overview
- **Returns**: Task list with IDs, titles, status, dependencies

#### Find Next Available Task
- **Purpose**: Identify next actionable task based on dependencies
- **MCP Tool**: `next_task`
- **CLI**: `task-master next`
- **Usage Context**: Work session planning, dependency-aware task selection
- **Logic**: Checks completed dependencies, priority, and ID order

#### View Task Details
- **Purpose**: Examine specific task implementation requirements
- **MCP Tool**: `get_task`
- **CLI**: `task-master show <id>`
- **Key Parameters**: `id` (task/subtask ID), `status` (filter subtasks)
- **Usage Context**: Implementation planning, requirement clarification
- **Supports**: Dot notation for subtasks (e.g., "15.2")

### Task Creation & Modification

#### Add New Task
- **Purpose**: Create new tasks with AI assistance
- **MCP Tool**: `add_task`
- **CLI**: `task-master add-task`
- **Key Parameters**: `prompt` (description), `research` (enhanced AI), `priority`
- **Usage Context**: New requirements, discovered work
- **Important**: AI operation taking 30-60 seconds

#### Add Subtask
- **Purpose**: Break down tasks or reorganize hierarchy
- **MCP Tool**: `add_subtask`
- **CLI**: `task-master add-subtask`
- **Key Parameters**: `id` (parent), `title`, `description`, `taskId` (convert existing)
- **Usage Context**: Task breakdown, hierarchy reorganization

#### Update Multiple Tasks
- **Purpose**: Apply changes to multiple future tasks
- **MCP Tool**: `update`
- **CLI**: `task-master update`
- **Key Parameters**: `from` (starting task ID), `prompt` (change description)
- **Usage Context**: Implementation pivots, architectural changes
- **Important**: AI operation, affects tasks with ID >= from value

#### Update Single Task
- **Purpose**: Modify specific task with new information
- **MCP Tool**: `update_task`
- **CLI**: `task-master update-task`
- **Key Parameters**: `id`, `prompt` (new information), `research`
- **Usage Context**: Requirement clarification, implementation refinement
- **Important**: AI operation taking 30-60 seconds

#### Update Subtask Details
- **Purpose**: Append implementation notes and progress logs
- **MCP Tool**: `update_subtask`
- **CLI**: `task-master update-subtask`
- **Key Parameters**: `id` (subtask ID), `prompt` (notes to append)
- **Usage Context**: Progress logging, implementation documentation
- **Behavior**: Appends timestamped information, preserves existing content

### Task Status Management

#### Set Task Status
- **Purpose**: Mark task completion and progress
- **MCP Tool**: `set_task_status`
- **CLI**: `task-master set-status`
- **Key Parameters**: `id`, `status` (pending|done|in-progress|review|deferred|cancelled)
- **Usage Context**: Progress tracking, workflow management
- **Supports**: Comma-separated IDs for batch updates

### Task Analysis & Expansion

#### Analyze Project Complexity
- **Purpose**: Evaluate task complexity for breakdown planning
- **MCP Tool**: `analyze_project_complexity`
- **CLI**: `task-master analyze-complexity`
- **Key Parameters**: `threshold` (1-10), `research` (AI-enhanced), `ids` (specific tasks)
- **Usage Context**: Pre-expansion analysis, breakdown planning
- **Output**: Complexity scores and expansion recommendations

#### View Complexity Report
- **Purpose**: Display formatted complexity analysis
- **MCP Tool**: `complexity_report`
- **CLI**: `task-master complexity-report`
- **Usage Context**: Understanding task complexity, planning breakdown depth

#### Expand Single Task
- **Purpose**: Break complex tasks into actionable subtasks
- **MCP Tool**: `expand_task`
- **CLI**: `task-master expand`
- **Key Parameters**: `id`, `num` (subtask count), `force` (replace existing), `research`
- **Usage Context**: Task breakdown, implementation planning
- **Important**: AI operation, uses complexity analysis if available

#### Expand All Pending Tasks
- **Purpose**: Bulk expansion of multiple tasks
- **MCP Tool**: `expand_all`
- **CLI**: `task-master expand --all`
- **Key Parameters**: `force`, `research`, `num` (override defaults)
- **Usage Context**: Project-wide task breakdown, batch processing

### Dependency Management

#### Add Task Dependency
- **Purpose**: Create prerequisite relationships between tasks
- **MCP Tool**: `add_dependency`
- **CLI**: `task-master add-dependency`
- **Key Parameters**: `id` (dependent task), `dependsOn` (prerequisite task)
- **Usage Context**: Workflow ordering, prerequisite establishment

#### Remove Task Dependency
- **Purpose**: Remove prerequisite relationships
- **MCP Tool**: `remove_dependency`
- **CLI**: `task-master remove-dependency`
- **Key Parameters**: `id`, `dependsOn`
- **Usage Context**: Workflow adjustments, dependency corrections

#### Validate Dependencies
- **Purpose**: Check for circular references and invalid links
- **MCP Tool**: `validate_dependencies`
- **CLI**: `task-master validate-dependencies`
- **Usage Context**: Quality assurance, dependency verification
- **Returns**: Issues found without making changes

#### Fix Dependencies
- **Purpose**: Automatically repair dependency issues
- **MCP Tool**: `fix_dependencies`
- **CLI**: `task-master fix-dependencies`
- **Usage Context**: Dependency repair, automated cleanup

### Task Organization

#### Move Tasks
- **Purpose**: Reorganize task hierarchy and ordering
- **MCP Tool**: `move_task`
- **CLI**: `task-master move`
- **Key Parameters**: `from` (source IDs), `to` (destination IDs)
- **Usage Context**: Hierarchy restructuring, order adjustment
- **Supports**: Comma-separated IDs for batch moves

#### Remove Tasks
- **Purpose**: Delete tasks or subtasks permanently
- **MCP Tool**: `remove_task`
- **CLI**: `task-master remove-task`
- **Key Parameters**: `id`, `confirm` (skip confirmation)
- **Usage Context**: Cleanup, scope reduction
- **Supports**: Comma-separated IDs for batch removal

#### Clear Subtasks
- **Purpose**: Remove all subtasks from parent tasks
- **MCP Tool**: `clear_subtasks`
- **CLI**: `task-master clear-subtasks`
- **Key Parameters**: `id` (task IDs), `all` (clear from all tasks)
- **Usage Context**: Subtask regeneration preparation, cleanup

### File Generation

#### Generate Task Files
- **Purpose**: Create individual task files from tasks.json
- **MCP Tool**: `generate`
- **CLI**: `task-master generate`
- **Usage Context**: File system synchronization, external tool integration
- **Output**: Individual `.md` files in tasks/ directory

## Parameter Patterns

### Common Parameters
- `projectRoot`: Absolute path to project directory (MCP required)
- `file`: Custom path to tasks.json (defaults to tasks/tasks.json)
- `id`: Task or subtask identifier (supports dot notation: "15.2")
- `prompt`: Descriptive text for AI operations
- `research`: Enable enhanced AI analysis with Perplexity
- `force`: Override existing data without confirmation

### ID Formats
- Task ID: `"15"` (single number)
- Subtask ID: `"15.2"` (parent.child format)
- Multiple IDs: `"15,16,17"` (comma-separated)

### Status Values
- `pending`: Ready to work on
- `done`: Completed and verified
- `in-progress`: Currently being worked on
- `review`: Awaiting review or approval
- `deferred`: Postponed to later
- `cancelled`: No longer needed

## AI Operation Indicators

Tools that perform AI processing and may take 30-60 seconds:
- `parse_prd`: Document analysis and task generation
- `add_task`: Intelligent task structuring
- `expand_task`: Smart subtask breakdown
- `expand_all`: Bulk task expansion
- `update_task`: Context-aware task updates
- `update_subtask`: Intelligent note appending
- `update`: Multi-task batch updates
- `analyze_project_complexity`: Complexity evaluation

## Configuration Requirements

### API Keys (Required for AI Operations)
- **MCP Context**: Configure in `.cursor/mcp.json` env section
- **CLI Context**: Configure in project `.env` file
- **Supported**: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `PERPLEXITY_API_KEY`, `GOOGLE_API_KEY`

### Model Configuration
- **File**: `.taskmasterconfig` in project root
- **Management**: Use `models` tool/command, never edit manually
- **Settings**: Model selections, parameters, logging level

## Common Workflow Sequences

### Project Initialization
1. `initialize_project` - Set up structure
2. Create PRD in `scripts/` directory
3. `parse_prd` - Generate initial tasks
4. `get_tasks` - Review generated tasks

### Development Session
1. `get_tasks` - Check current status
2. `next_task` - Find next actionable work
3. `get_task` - Review implementation details
4. Code implementation
5. `set_task_status` - Mark as done

### Task Breakdown
1. `analyze_project_complexity` - Evaluate complexity
2. `complexity_report` - Review analysis
3. `expand_task` - Break down complex tasks
4. `validate_dependencies` - Check relationships

### Implementation Changes
1. `update_task` - Modify specific task
2. `update` - Update dependent tasks
3. `generate` - Sync file system
4. `validate_dependencies` - Verify integrity

This reference enables GitHub Copilot to provide contextually appropriate suggestions and understand the Task Master workflow integration.
