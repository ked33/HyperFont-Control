// Host-only parser/identity regression; never packaged into the module.
#include "font_compat.cpp"

int main() {
    char good[]="hyperfont-exempt-v1\nsdk=31\nuid=10001\npkg=10001:com.app.test\nxml=/system/etc/fonts.xml\n";
    exemption::Manifest m;
    if(!exemption::parse(good,m))return 1;
    if(!exemption::matches(m,10001,"/data/user/0/com.app.test"))return 2;
    if(!exemption::matches(m,110001,"/data/user/1/com.app.test"))return 3;
    if(exemption::matches(m,10001,"/data/user/0/com.app.other"))return 4;
    if(exemption::matches(m,99001,"/data/user/0/com.app.test"))return 5;
    if(exemption::matches(m,1000,"/data/user/0/com.app.test"))return 6;
    if(exemption::matches(m,10001,nullptr))return 7;
    char duplicate[]="hyperfont-exempt-v1\nsdk=31\nuid=10001\nuid=10001\nxml=/system/etc/fonts.xml\n";
    exemption::Manifest d;
    if(exemption::parse(duplicate,d))return 8;
    char badPath[]="hyperfont-exempt-v1\nsdk=31\nuid=10001\nxml=/system/etc/../fonts.xml\n";
    exemption::Manifest p;
    if(exemption::parse(badPath,p))return 9;
    char future[]="hyperfont-exempt-v1\nsdk=36\nuid=10001\nxml=/system/etc/fonts.xml\n";
    exemption::Manifest f;
    if(exemption::parse(future,f))return 10;
    char injected[]="hyperfont-exempt-v1\nsdk=31\nuid=10001\npkg=10001:com/app\nxml=/system/etc/fonts.xml\n";
    exemption::Manifest i;
    if(exemption::parse(injected,i))return 11;
    return 0;
}
