// Startup-only process isolation. No text, Paint, Skia or app methods are hooked.
extern "C" int unshare(int);
extern "C" int mount(const char *, const char *, const char *, unsigned long, const void *);
extern "C" int umount2(const char *, int);
extern "C" long lseek(int, long, int);
extern "C" int dup2(int, int);
extern "C" long readlink(const char *, char *, unsigned long);
extern "C" long write(int, const void *, unsigned long);
extern "C" int getpid();
extern "C" int ioctl(int, unsigned long, ...);
extern "C" int ftruncate(int, long);
extern "C" void *mmap(void *, unsigned long, int, int, int, long);
extern "C" int munmap(void *, unsigned long);

namespace exemption {
constexpr char root[] = "/dev/hyperfont-control/exemptions";
constexpr unsigned maxPaths = 12;
constexpr unsigned maxIDs = 128;
#if defined(__aarch64__)
constexpr int directoryFlag=040000;
#else
constexpr int directoryFlag=0200000;
#endif
struct Manifest {
    int sdk{};
    int ids[maxIDs]{};
    char packages[maxIDs][164]{};
    int packageIDs[maxIDs]{};
    unsigned packageCount{};
    unsigned count{};
    char paths[maxPaths][192]{};
    unsigned pathCount{};
};

unsigned length(const char *s) { unsigned n=0; while (s[n]) ++n; return n; }
bool equal(const char *a,const char *b) { unsigned i=0; while(a[i] && a[i]==b[i])++i; return a[i]==b[i]; }
bool prefix(const char *s,const char *p) { for(unsigned i=0;p[i];++i) if(s[i]!=p[i])return false; return true; }
bool append(char *out,unsigned capacity,const char *a,const char *b) {
    unsigned n=length(a),m=length(b);
    if(n+m>=capacity)return false;
    for(unsigned i=0;i<n;++i)out[i]=a[i];
    for(unsigned i=0;i<=m;++i)out[n+i]=b[i];
    return true;
}
void decimal(unsigned value,char *out) {
    char reverse[16];unsigned n=0;
    do{reverse[n++]=char('0'+value%10);value/=10;}while(value);
    for(unsigned i=0;i<n;++i)out[i]=reverse[n-1-i];out[n]=0;
}
int number(const char *s) {
    unsigned n=0;if(!*s)return -1;
    for(;*s;++s) {if(*s<'0'||*s>'9'||n>1000000)return -1;n=n*10+unsigned(*s-'0');}
    return int(n);
}
bool permittedPath(const char *path) {
    const char *allowed[]={"/system/etc/fonts.xml","/system/etc/font_fallback.xml",
        "/system_ext/etc/hyper_fonts.xml","/system_ext/etc/hyper_font_fallback.xml",
        "/system_ext/etc/miui_fonts.xml","/system_ext/etc/miui_font_fallback.xml",
        "/product/etc/fonts_customization.xml","/product/etc/misans_variable_fonts.xml",
        "/product/etc/mi_fonts_customization.xml"};
    for(const auto *p:allowed)if(equal(p,path))return true;
    return false;
}
bool parse(char *bytes,Manifest &m) {
    char *line=bytes;unsigned index=0;
    for(char *p=bytes;;++p) {
        if(*p!='\n'&&*p)continue;
        bool end=!*p;*p=0;
        if(index++==0){if(!equal(line,"hyperfont-exempt-v1"))return false;}
        else if(prefix(line,"sdk=")){if(m.sdk)return false;m.sdk=number(line+4);}
        else if(prefix(line,"uid=")){
            int id=number(line+4);if(id<10000||id>=90000||m.count>=maxIDs)return false;
            for(unsigned i=0;i<m.count;++i)if(m.ids[i]==id)return false;
            m.ids[m.count++]=id;
        } else if(prefix(line,"pkg=")) {
            char *split=line+4;while(*split&&*split!=':')++split;
            if(!*split||m.packageCount>=maxIDs)return false;*split++=0;
            int id=number(line+4);if(id<10000||id>=90000||!*split)return false;
            for(char *c=split;*c;++c)if(!((*c>='a'&&*c<='z')||(*c>='A'&&*c<='Z')||(*c>='0'&&*c<='9')||*c=='_'||*c=='.'))return false;
            m.packageIDs[m.packageCount]=id;
            if(!append(m.packages[m.packageCount++],164,split,""))return false;
        } else if(prefix(line,"xml=")) {
            if(m.pathCount>=maxPaths||!permittedPath(line+4))return false;
            for(unsigned i=0;i<m.pathCount;++i)if(equal(m.paths[i],line+4))return false;
            if(!append(m.paths[m.pathCount++],192,line+4,""))return false;
        } else if(*line)return false;
        if(end)break;line=p+1;
    }
    return m.sdk>=31&&m.sdk<=35&&m.count>0&&m.pathCount>0;
}
bool load(Manifest &m) {
    char path[256];append(path,sizeof(path),root,"/manifest");
    int fd=open(path,0);if(fd<0)return false;
    char bytes[32769];long size=read(fd,bytes,sizeof(bytes)-1);close(fd);
    if(size<=0||size>=long(sizeof(bytes)-1))return false;
    for(long i=0;i<size;++i)if(!bytes[i])return false;
    bytes[size]=0;return parse(bytes,m);
}
bool matches(const Manifest &m,int uid,const char *dataDir) {
    if(uid<0)return false;int id=uid%100000;
    if(!dataDir||!*dataDir)return false;
    const char *package=dataDir;
    for(const char *p=dataDir;*p;++p)if(*p=='/')package=p+1;
    bool allowed=false;for(unsigned i=0;i<m.count;++i)if(m.ids[i]==id)allowed=true;
    if(!allowed)return false;
    for(unsigned i=0;i<m.packageCount;++i)if(m.packageIDs[i]==id&&equal(m.packages[i],package))return true;
    return false;
}

// Opened under root, retained only until the one shared-memory substitution.
int mapFD=-1,reportFD=-1;
char *reportPage=nullptr;
unsigned reportLength=0;
jint mapSize=0;
using SizeFn=jint(*)(JNIEnv*,jobject,jobject);
SizeFn originalSize=nullptr;
jfieldID descriptorField=nullptr;
bool consumed=false;
#ifdef HFC_DIAGNOSTICS
char lastFDName[192]{};
#endif

void milestone(const char *text) {
    // Mapped under root before sandboxing. Later milestones only touch the
    // shared page, avoiding post-specialization writes to root-owned files.
    unsigned n=length(text);
    if(reportPage && reportLength+n+2<4096) {
        for(unsigned i=0;i<n;++i)reportPage[reportLength++]=text[i];
        reportPage[reportLength++]='\n';reportPage[reportLength]=0;
    }
}
void startReport(int uid) {
    char u[16],p[16],name[48],path[256];decimal(unsigned(uid),u);decimal(unsigned(getpid()),p);
    append(name,sizeof(name),u,"-");unsigned n=length(name);append(name+n,sizeof(name)-n,p,"");
    char dir[256];append(dir,sizeof(dir),root,"/results/");append(path,sizeof(path),dir,name);
    // Bionic O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC. Parent is root-only tmpfs.
    reportFD=open(path,2|64|512|02000000,0600);
    if(reportFD>=0) {
        if(reportFD>=0 && ftruncate(reportFD,4096)==0) {
            void *page=mmap(nullptr,4096,3,1,reportFD,0);
            if(page!=reinterpret_cast<void*>(-1))reportPage=static_cast<char*>(page);
        }
        if(reportFD>=0)close(reportFD);
        reportFD=-1;
    }
    char stat[2048];int fd=open("/proc/self/stat",0);long size=fd<0?-1:read(fd,stat,sizeof(stat)-1);if(fd>=0)close(fd);
    if(size>0){stat[size]=0;char *after=nullptr;for(char *c=stat;*c;++c)if(*c==')')after=c+1;
        if(after){unsigned field=3;char *c=after;while(*c&&field<=22){while(*c==' ')++c;char *begin=c;while(*c&&*c!=' ')++c;
            if(field==22){*c=0;char line[64];append(line,sizeof(line),"start=",begin);milestone(line);break;}++field;}}
    }
}
void closeResources() {if(mapFD>=0){close(mapFD);mapFD=-1;}if(reportFD>=0){close(reportFD);reportFD=-1;}if(reportPage){munmap(reportPage,4096);reportPage=nullptr;}}

int restoreStep=0;
bool restoreFiles(const Manifest &m) {
    // Only in forked app process, never system_server/zygote. Make the entire
    // namespace private BEFORE binding so no mount can propagate to another app.
    restoreStep=1;
    if(unshare(0x00020000)!=0)return false; // CLONE_NEWNS
    restoreStep=2;
    if(mount(nullptr,"/",nullptr,(1UL<<18)|16384,nullptr)!=0)return false;
    restoreStep=3;
    char sources[maxPaths+2][384]{};char targets[maxPaths+2][192]{};unsigned count=0;
    const char *dirs[]={"/system/fonts","/product/fonts"};
    for(const char *dir:dirs) {
        char source[384];append(source,sizeof(source),"/dev/hyperfont-control/original",dir);
        int fd=open(source,directoryFlag);if(fd<0){if(equal(dir,"/product/fonts"))continue;return false;}close(fd);
        append(sources[count],384,source,"");append(targets[count++],192,dir,"");
    }
    for(unsigned i=0;i<m.pathCount;++i) {
        char dir[256];append(dir,sizeof(dir),root,"/original");append(sources[count],384,dir,m.paths[i]);
        append(targets[count++],192,m.paths[i],"");
    }
    unsigned done=0;
    for(;done<count;++done) {
        restoreStep=10+int(done);
        unsigned long flags=4096; if(done<2 && prefix(targets[done],"/system/fonts"))flags|=16384;
        if(equal(targets[done],"/product/fonts"))flags|=16384;
        if(mount(sources[done],targets[done],nullptr,flags,nullptr)!=0)break;
    }
    if(done!=count){while(done>0)umount2(targets[--done],2);return false;}
    milestone("files-restored");return true;
}

void restoreHook(JNIEnv *env) {
    JNINativeMethod method{const_cast<char*>("nGetSize"),const_cast<char*>("(Ljava/io/FileDescriptor;)I"),reinterpret_cast<void*>(originalSize)};
    jclass shared=env->FindClass("android/os/SharedMemory");
    if(shared&&env->RegisterNatives(shared,&method,1)==0)milestone("initialization-hook-removed");
    if(env->ExceptionCheck())env->ExceptionClear();
    if(shared)env->DeleteLocalRef(shared);
}
jint replaceSize(JNIEnv *env,jobject object,jobject descriptor) {
    // The name belongs to framework's serialized font map; other ashmem is
    // forwarded unchanged. No nativeReadTypefaces ABI assumptions or name-table
    // rewriting: the FULL buffer (including family names) is substituted.
    int fd=env->GetIntField(descriptor,descriptorField);
    char numberText[16],path[64],name[192];decimal(unsigned(fd),numberText);append(path,sizeof(path),"/proc/self/fd/",numberText);
    long size=readlink(path,name,sizeof(name)-1);if(size>=0)name[size]=0;
#ifdef HFC_DIAGNOSTICS
    if(size>0)append(lastFDName,sizeof(lastFDName),name,"");
#endif
    bool fontMap=size>0&&(equal(name,"/memfd:fontMap (deleted)")||equal(name,"/dev/ashmem/fontMap (deleted)")||equal(name,"/dev/ashmem/fontMap"));
    // Legacy ashmem exposes a device path rather than the region name. Read
    // the kernel region name; never substitute an arbitrary ashmem descriptor.
    if(!fontMap && size>0 && prefix(name,"/dev/ashmem")) {
        char region[256]{};
        if(ioctl(fd,0x81007702UL,region)==0) {region[255]=0;fontMap=equal(region,"fontMap");}
    }
    jint result=originalSize(env,object,descriptor);
    if(!consumed&&fontMap&&result>0&&mapFD>=0) {
        consumed=true;
        if(dup2(mapFD,fd)>=0){result=mapSize;milestone("font-map-substituted");}
        else milestone("font-map-substitution-failed");
        restoreHook(env);closeResources();
    }
    return result;
}
bool prepare(JNIEnv *env,zygisk::Api *api,const Manifest &m,int uid) {
    startReport(uid);
    char path[256];append(path,sizeof(path),root,"/stock-font-map");
    mapFD=open(path,0|02000000);
    long size=mapFD<0?-1:lseek(mapFD,0,2);
    if(size<8||size>(32<<20)){milestone("map-unavailable");closeResources();return false;}
    mapSize=jint(size);
    jclass sdk=env->FindClass("android/os/Build$VERSION");
    jfieldID sdkField=sdk?env->GetStaticFieldID(sdk,"SDK_INT","I"):nullptr;
    int actualSDK=sdkField?env->GetStaticIntField(sdk,sdkField):0;
    if(sdk)env->DeleteLocalRef(sdk);
    jclass descriptor=env->FindClass("java/io/FileDescriptor");
    descriptorField=descriptor?env->GetFieldID(descriptor,"descriptor","I"):nullptr;
    if(descriptor)env->DeleteLocalRef(descriptor);
    if(env->ExceptionCheck()){env->ExceptionClear();descriptorField=nullptr;}
    if(!descriptorField||actualSDK!=m.sdk||!api->exemptFd(mapFD)) {
        milestone("unsupported-initialization");closeResources();return false;
    }
    JNINativeMethod method{const_cast<char*>("nGetSize"),const_cast<char*>("(Ljava/io/FileDescriptor;)I"),reinterpret_cast<void*>(replaceSize)};
    api->hookJniNativeMethods(env,"android/os/SharedMemory",&method,1);
    originalSize=reinterpret_cast<SizeFn>(method.fnPtr);
    if(!originalSize){milestone("initialization-hook-unavailable");closeResources();return false;}
    if(!restoreFiles(m)) {milestone("files-restore-failed");restoreHook(env);closeResources();return true;}
    milestone("awaiting-font-map");return true;
}
} // namespace exemption
