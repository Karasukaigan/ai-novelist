import os
import shutil
import aiofiles
import uuid
import re
import logging
from pathlib import Path
from typing import List, Dict, Any
from fastapi import HTTPException, UploadFile
from natsort import natsorted
from backend.config.config import settings
from backend.file.ripgrep_service import ripgrep_service

logger = logging.getLogger(__name__)
def sort_items(items: List[Dict]) -> List[Dict]:
    """对项目列表进行自动排序"""
    # 按名称自然顺序排序
    sorted_items = natsorted(items, key=lambda item: item["title"])
    
    # 文件夹在前，文件在后
    folders = [item for item in sorted_items if item.get("isFolder", False)]
    files = [item for item in sorted_items if not item.get("isFolder", False)]
    
    return folders + files


async def get_file_tree(dir_path: str, base_dir_path: str, system_dirs: set = None) -> List[Dict]:
    """递归读取目录结构
    
    Args:
        dir_path: 要读取的目录路径
        base_dir_path: 基础目录路径，用于计算相对路径
        system_dirs: 要过滤的系统目录集合，如果为None则使用默认值
    
    Returns:
        文件树结构列表
    """
    # 系统目录列表，这些目录不会被显示在文件树中
    if system_dirs is None:
        system_dirs = {'config', 'chromadb', 'db', 'uploads', 'temp', 'skills'}
    
    entries = os.listdir(dir_path)
    result = []

    for entry_name in entries:
        # 跳过系统目录
        if entry_name in system_dirs:
            continue
            
        full_path = os.path.join(dir_path, entry_name)
        relative_path = os.path.relpath(full_path, base_dir_path)

        if os.path.isdir(full_path):
            children = await get_file_tree(full_path, base_dir_path, system_dirs)
            result.append({
                "id": relative_path.replace("\\", "/"),
                "title": entry_name,
                "isFolder": True,
                "children": sort_items(children)
            })
        else:
            result.append({
                "id": relative_path.replace("\\", "/"),
                "title": entry_name,
                "isFolder": False
            })

    sorted_result = sort_items(result)
    return sorted_result


async def get_file_tree_for_user(dir_path: str, base_dir_path: str) -> List[Dict]:
    """递归读取目录结构（用于前端用户）
    
    过滤的系统目录：{'chromadb', 'db', 'uploads', 'temp'}
    
    Args:
        dir_path: 要读取的目录路径
        base_dir_path: 基础目录路径，用于计算相对路径
    
    Returns:
        文件树结构列表
    """
    system_dirs = {'chromadb', 'db', 'uploads', 'temp'}
    return await get_file_tree(dir_path, base_dir_path, system_dirs)


async def get_file_tree_for_ai(dir_path: str, base_dir_path: str) -> List[Dict]:
    """递归读取目录结构（用于AI系统提示词）
    
    过滤的系统目录：{'config', 'chromadb', 'db', 'uploads', 'temp', 'skills'}
    
    Args:
        dir_path: 要读取的目录路径
        base_dir_path: 基础目录路径，用于计算相对路径
    
    Returns:
        文件树结构列表
    """
    system_dirs = {'config', 'chromadb', 'db', 'uploads', 'temp', 'skills'}
    return await get_file_tree(dir_path, base_dir_path, system_dirs)


async def generate_unique_name(target_dir: str, is_folder: bool = False) -> str:
    """生成唯一的文件或文件夹名称"""
    if is_folder:
        base_name = "新建文件夹"
        ext_name = ""
    else:
        base_name = "新建文件"
        ext_name = ""  # 不再强制添加 .md 后缀

    counter = 0
    unique_name = ""

    while True:
        counter += 1
        current_name = f"{base_name}{counter}{ext_name}"

        full_path = os.path.join(target_dir, current_name)

        if not os.path.exists(full_path):
            unique_name = current_name
            break
    return unique_name

async def create_item(is_folder: bool = False, parent_path: str = "") -> Dict[str, Any]:
    """创建文件或文件夹"""
    target_dir = os.path.join(settings.DATA_DIR, parent_path)
    unique_name = await generate_unique_name(target_dir, is_folder)
    item_path = os.path.join(target_dir, unique_name)

    # 根据类型创建文件或文件夹
    if is_folder:
        os.makedirs(item_path, exist_ok=True)
    else:
        async with aiofiles.open(item_path, 'w', encoding='utf-8') as f:
            await f.write("")

    # 获取文件状态信息
    relative_id = os.path.relpath(item_path, settings.DATA_DIR)

    # 构建返回结果
    result = {
        "id": relative_id,
        "title": unique_name,
        "path": item_path,
        "type": "folder" if is_folder else "file"
    }

    return result


async def read_file(file_path: str) -> str:
    """读取文件内容"""
    full_path = Path(settings.DATA_DIR) / file_path
    # 如果文件不存在，返回空字符串（用于AI创建新文件的场景）
    if not full_path.exists():
        return ''
    async with aiofiles.open(full_path, 'r', encoding='utf-8') as f:
        content = await f.read()
    return content


async def update_file(file_path: str, content: str):
    """更新文件内容"""
    full_path = Path(settings.DATA_DIR) / file_path
    # 自动创建父文件夹（如果不存在）
    full_path.parent.mkdir(parents=True, exist_ok=True)
    # 将 \r\n 转换为 \n，避免Windows换行符问题
    content = content.replace('\r\n', '\n')
    async with aiofiles.open(full_path, 'w', encoding='utf-8') as f:
        await f.write(content)


async def delete_file(file_path: str):
    """删除文件或文件夹"""
    full_path = Path(settings.DATA_DIR) / file_path
    if os.path.isdir(full_path):
        shutil.rmtree(full_path)
    else:
        os.remove(full_path)

async def rename_file(old_path: str, new_name: str):
    """重命名文件或文件夹"""
    full_old_path = Path(settings.DATA_DIR) / old_path
    parent_dir = os.path.dirname(full_old_path)
    new_path = os.path.join(parent_dir, new_name)
    # 检查目标路径是否已存在
    if os.path.exists(new_path):
        raise HTTPException(
            status_code=400,
            detail=f"目标已存在: {new_path}"
        )
    os.rename(full_old_path, new_path)


# 经测试，copy_file如果出现错误，尤其是“文件/文件夹已存在”时，控制台显示错误，但是并不会被FastAPI正确处理，只会显示TypeError。所以直接在函数内提前验证，及时抛出错误。
# move_file则可以被捕获错误，但最好统一成copy_file的形式（问题根源可能是shutil的move和copy方法不同）

async def move_file(source_path: str, target_path: str):
    """移动文件或文件夹"""
    full_source = Path(settings.DATA_DIR) / source_path
    full_target_dir = Path(settings.DATA_DIR) / target_path
    source_name = os.path.basename(full_source)
    full_target_path = os.path.join(full_target_dir, source_name)
    print("完整来源路径:",full_source,"完整目标路径：",full_target_dir)
    # 检查目标路径是否已存在
    if os.path.exists(full_target_path):
        raise HTTPException(
            status_code=400,
            detail=f"目标已存在: {full_target_path}"
        )

    shutil.move(full_source, full_target_path)


async def copy_file(source_path: str, target_path: str):
    """复制文件或文件夹，如果目标已存在则失败"""
    full_source = Path(settings.DATA_DIR) / source_path # 原路径
    full_target_dir = Path(settings.DATA_DIR) / target_path # 目标目录
    source_name = os.path.basename(full_source)
    full_target_path = os.path.join(full_target_dir, source_name) # 最终形成的新路径（目标目录+文件名/文件夹名）
    print("完整来源路径:",full_source,"完整目标路径：",full_target_dir)
    # 检查目标路径是否已存在
    if os.path.exists(full_target_path):
        raise HTTPException(
            status_code=400,
            detail=f"目标已存在: {full_target_path}"
        )

    if os.path.isdir(full_source):
        shutil.copytree(full_source, full_target_path)
    else:
        shutil.copy2(full_source, full_target_path)


async def search_files(query: str, system_dirs: set = None) -> List[Dict[str, Any]]:
    """搜索文件内容（使用 ripgrep）
    
    Args:
        query: 搜索关键词
        system_dirs: 要过滤的系统目录集合，如果为None则使用默认值
    
    Returns:
        搜索结果列表
    """
    # 使用 ripgrep 搜索（通过 .gitignore 过滤系统目录）
    try:
        return await ripgrep_service.search(
            query=query,
            case_sensitive=False
        )
    except Exception as e:
        logger.error(f"ripgrep 搜索失败: {e}")
        return ""


async def search_files_for_user(query: str) -> Dict[str, Dict[str, Any]]:
    """搜索文件内容（用于前端用户）
    
    过滤的系统目录：{'chromadb', 'db', 'uploads', 'temp'}
    
    Args:
        query: 搜索关键词
    
    Returns:
        搜索结果字典，按文件路径分组
        格式: {
            "文件路径1": {
                "path": "文件路径1",
                "content": ["内容1", "内容2", ...]
            },
            ...
        }
    """
    system_dirs = {'chromadb', 'db', 'uploads', 'temp'}
    rg_output = await search_files(query, system_dirs)
    
    # 解析 ripgrep 输出，按文件路径分组
    results = {}
    for line in rg_output.split('\n'):
        if not line.strip() or line == '--':
            continue
        
        # 匹配格式：文件路径:行号:内容
        match = re.match(r'^(.+?):(\d+):(.*)$', line)
        if match:
            file_path = match.group(1)
            line_number = match.group(2)
            line_content = match.group(3)
            
            # 去除 backend\data\ 或 backend/data/ 前缀，只保留相对路径
            if file_path.startswith('backend\\data\\'):
                file_path = file_path[len('backend\\data\\'):]
            elif file_path.startswith('backend/data/'):
                file_path = file_path[len('backend/data/'):]
            
            # 统一路径分隔符为 /
            file_path = file_path.replace('\\', '/')
            
            # 按文件路径分组
            if file_path not in results:
                results[file_path] = {
                    "path": file_path,
                    "content": []
                }
            results[file_path]["content"].append(line_content)
    
    return results


async def search_files_for_ai(query: str) -> List[Dict[str, Any]]:
    """搜索文件内容（用于AI工具）
    
    过滤的系统目录：{'config', 'chromadb', 'db', 'uploads', 'temp', 'skills'}
    
    Args:
        query: 搜索关键词
    
    Returns:
        搜索结果列表
    """
    system_dirs = {'config', 'chromadb', 'db', 'uploads', 'temp', 'skills'}
    return await search_files(query, system_dirs)


async def upload_image(file: UploadFile) -> Dict[str, Any]:
    """上传图片文件"""
    upload_dir = Path("backend") / "data" / "uploads"
    allowed_extensions = {'.jpg', '.jpeg', '.png', '.gif', '.svg', '.webp'}

    upload_dir.mkdir(parents=True, exist_ok=True)
    ext = Path(file.filename).suffix.lower()
    if ext not in allowed_extensions:
        raise HTTPException(
            status_code=400,
            detail=f"不支持的文件类型。仅支持 {', '.join(allowed_extensions)} 格式。"
        )
    
    content = await file.read()
    # 生成唯一文件名
    timestamp = int(os.times().elapsed * 1000)
    random_str = uuid.uuid4().hex[:8]
    filename = f"image_{timestamp}_{random_str}{ext}"
    file_path = upload_dir / filename

    # 写入文件
    async with aiofiles.open(file_path, 'wb') as f:
        await f.write(content)

    return {
        "filename": filename,
        "url": f"http://{settings.HOST}:{settings.PORT}/uploads/{filename}"
    }
